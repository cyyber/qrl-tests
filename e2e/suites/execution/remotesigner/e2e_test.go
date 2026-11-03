//go:build e2e

package remotesigner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/cyyber/qrl-tests/e2e/internal/live"
	"github.com/cyyber/qrl-tests/e2e/internal/remotesigner"
	"github.com/cyyber/qrl-tests/e2e/internal/testsuite"
	"github.com/cyyber/qrl-tests/internal/dockerapi"
	dockerclient "github.com/moby/moby/client"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	qrlaccounts "github.com/theQRL/go-qrl/accounts"
	"github.com/theQRL/go-qrl/common"
	"github.com/theQRL/go-qrl/common/hexutil"
	"github.com/theQRL/go-qrl/core/types"
	"github.com/theQRL/go-qrl/params"
	"github.com/theQRL/go-qrl/qrlclient"
	"github.com/theQRL/go-qrl/rpc"
	walletmldsa87 "github.com/theQRL/go-qrllib/wallet/ml_dsa_87"
)

const (
	// diagnosticsDirectory, in the lane's report directory, receives what
	// the signer reports when a spec fails.
	diagnosticsDirectory = "execution-signer"

	// wrongChainID is the chain the refused transactions are made for.
	wrongChainID = 1
	// message is what the signer signs with qrl_sign.
	message = "go-remotesigner execution end-to-end"

	minedTimeout = time.Minute
	pollInterval = time.Second
)

// Fees of the transaction the fee cap refuses: gas × maxFeePerGas is 10^7 ×
// 10^12 Planck, 10 quanta, above the signer's cap of 1 quanta, which the
// policy qrl-package gives it keeps.
var (
	overCapGas       = hexutil.Uint64(10_000_000)
	overCapFeePerGas = (*hexutil.Big)(big.NewInt(1_000_000_000_000))
	feeCap           = big.NewInt(params.Quanta)
)

var recipient = common.BytesToAddress([]byte("execution remote signer e2e recipient"))

// The methods of the proxy's allowlist checks: forwarded, the network's
// version; refused, a method of each namespace the default allowlist keeps
// from the execution client, with the parameters the client would answer.
var (
	forwardedMethod = "net_version"
	refusedMethods  = []struct {
		method string
		params []any
		// node is whether the execution client answers the method itself:
		// it serves the admin and debug APIs, not the personal one.
		node bool
	}{
		{"admin_nodeInfo", nil, true},
		{"debug_getRawHeader", []any{"latest"}, true},
		{"personal_listAccounts", nil, false},
	}
)

func TestE2E(t *testing.T) {
	testsuite.Run(t, "Execution remote signer E2E suite")
}

var _ = ginkgo.Describe(
	"go-remotesigner in execution mode in front of the execution client",
	ginkgo.Serial,
	ginkgo.Ordered,
	ginkgo.ContinueOnFailure,
	ginkgo.Label("e2e", "execution", "remote-signer"),
	func() {
		var (
			node    *live.Node
			docker  dockerclient.APIClient
			service devnet.RemoteSignerService
			signer  *remotesigner.Client
			// rpcClient calls the signer's JSON-RPC endpoint, and proxied is
			// a typed client over it, whose calls the signer forwards.
			rpcClient *rpc.Client
			proxied   *qrlclient.Client
			txSigner  types.Signer
			// accounts are the accounts the signer holds; from, the first,
			// sends and signs the transactions.
			accounts []remotesigner.ExecutionAccount
			from     common.Address
			// initial is the signer's metrics before the specs, and
			// initialAuditEntries the number of entries its audit log had.
			initial             remotesigner.Metrics
			initialAuditEntries int
			// calls counts the JSON-RPC calls the specs made to the signer
			// with its token.
			calls int

			// What the specs had the signer sign, for the audit log.
			sentHash, signedHash common.Hash
		)

		scrape := func(ctx context.Context) remotesigner.Metrics {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(remotesigner.Scrape(ctx, service.MetricsURL))
		}

		sum := func(metrics remotesigner.Metrics, name string, match remotesigner.Matcher) float64 {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(metrics.Sum(name, match))
		}

		call := func(ctx context.Context, result any, method string, args ...any) error {
			calls++
			return rpcClient.CallContext(ctx, result, method, args...)
		}

		// served is how many calls of method the execution client served,
		// successfully or not, by its own metrics; it counts no call of a
		// method it does not have.
		served := func(ctx context.Context, method string) float64 {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(servedCalls(ctx, node.Participant.Execution.MetricsURL, method))
		}

		readAuditLog := func(ctx context.Context) ([]byte, error) {
			return devnet.ReadServiceFile(ctx, docker, service.ID, devnet.ExecutionSignerAuditLogFile)
		}

		auditLog := func(ctx context.Context) []remotesigner.AuditEntry {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(remotesigner.ParseAuditLog(testsuite.MustSucceed(readAuditLog(ctx))))
		}

		pendingNonce := func(ctx context.Context) uint64 {
			ginkgo.GinkgoHelper()
			return testsuite.MustSucceed(node.Execution.PendingNonceAt(ctx, from))
		}

		// fees returns a tip and a fee cap that leaves room for the base fee
		// to double, as gqrl's qrl_fillTransaction does.
		fees := func(ctx context.Context) (tip, feeCap *big.Int) {
			ginkgo.GinkgoHelper()
			head := testsuite.MustSucceed(node.Execution.HeaderByNumber(ctx, nil))
			tip = testsuite.MustSucceed(node.Execution.SuggestGasTipCap(ctx))
			return tip, new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip)
		}

		// waitMined waits for the receipt, which it asks the signer for: the
		// signer forwards qrl_getTransactionReceipt.
		waitMined := func(ctx context.Context, hash common.Hash) *types.Receipt {
			ginkgo.GinkgoHelper()
			var receipt *types.Receipt
			gomega.Eventually(func(ctx context.Context) error {
				var err error
				receipt, err = proxied.TransactionReceipt(ctx, hash)
				return err
			}).WithContext(ctx).WithTimeout(minedTimeout).WithPolling(pollInterval).Should(gomega.Succeed(),
				"transaction %s was not mined", hash)
			return receipt
		}

		// expectRejected checks a refusal of a transaction: -32003, with the
		// reason in the error's data.
		expectRejected := func(err error, reason string) {
			ginkgo.GinkgoHelper()
			var rpcErr rpc.Error
			gomega.Expect(errors.As(err, &rpcErr)).To(gomega.BeTrue(), "the signer did not refuse the transaction: %v", err)
			gomega.Expect(rpcErr.ErrorCode()).To(gomega.Equal(remotesigner.CodeTransactionRejected), "%v", err)
			var dataErr rpc.DataError
			gomega.Expect(errors.As(err, &dataErr)).To(gomega.BeTrue(), "the refusal has no data: %v", err)
			gomega.Expect(fmt.Sprint(dataErr.ErrorData())).To(gomega.ContainSubstring(reason))
		}

		// transaction is a transaction object of from to the recipient, with
		// every field qrl_signTransaction needs.
		transaction := func(ctx context.Context, nonce uint64) map[string]any {
			ginkgo.GinkgoHelper()
			tip, feeCap := fees(ctx)
			return map[string]any{
				"from":                 from,
				"to":                   recipient,
				"value":                (*hexutil.Big)(big.NewInt(1)),
				"nonce":                hexutil.Uint64(nonce),
				"gas":                  hexutil.Uint64(params.TxGas),
				"maxFeePerGas":         (*hexutil.Big)(feeCap),
				"maxPriorityFeePerGas": (*hexutil.Big)(tip),
			}
		}

		ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
			runtime := testsuite.LoadRuntime()
			gomega.Expect(runtime.Profile.RunsExecutionSigner()).To(gomega.BeTrue(),
				"the lane's profile %q runs no go-remotesigner in execution mode", runtime.Profile)
			node = testsuite.MustSucceed(runtime.PrimaryNode(ctx))
			service = node.Participant.ExecutionSigner
			gomega.Expect(service.URL).NotTo(gomega.BeEmpty(),
				"the network runs no go-remotesigner in execution mode: start it with the %s profile", runtime.Profile)
			gomega.Expect(service.MetricsURL).NotTo(gomega.BeEmpty(), "the signer publishes no metrics port")
			gomega.Expect(node.Participant.Execution.MetricsURL).NotTo(gomega.BeEmpty(),
				"the execution client publishes no metrics port")
			txSigner = types.LatestSignerForChainID(node.ChainID)

			docker = testsuite.MustSucceed(dockerapi.New())
			ginkgo.DeferCleanup(docker.Close)
			// The signer's own files: the token is never logged.
			readFile := func(file string) []byte {
				ginkgo.GinkgoHelper()
				return testsuite.MustSucceed(devnet.ReadServiceFile(ctx, docker, service.ID, file))
			}
			token := strings.TrimSpace(string(readFile(devnet.ExecutionSignerTokenFile)))
			signer = testsuite.MustSucceed(remotesigner.New(service.URL, readFile(devnet.ExecutionSignerCAFile), token))
			rpcClient = testsuite.MustSucceed(signer.DialRPC(ctx))
			ginkgo.DeferCleanup(rpcClient.Close)
			proxied = qrlclient.NewClient(rpcClient)
			initial = scrape(ctx)
			initialAuditEntries = len(auditLog(ctx))
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.AfterEach(func(ctx ginkgo.SpecContext) {
			report := ginkgo.CurrentSpecReport()
			if !report.Failed() {
				return
			}
			if signer == nil {
				ginkgo.GinkgoWriter.Println("no remote signer diagnostics: the suite never reached the signer")
				return
			}
			dir := filepath.Join(testsuite.LaneReportDir(), diagnosticsDirectory, slug(report.LeafNodeText))
			if err := remotesigner.WriteExecutionDiagnostics(ctx, dir, signer, service.MetricsURL, readAuditLog); err != nil {
				ginkgo.GinkgoWriter.Printf("remote signer diagnostics are incomplete: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("remote signer diagnostics: %s\n", dir)
		}, ginkgo.NodeTimeout(time.Minute))

		ginkgo.It("serves its prefunded accounts, and only to its token", func(ctx ginkgo.SpecContext) {
			gomega.Expect(signer.Upcheck(ctx)).To(gomega.Succeed())
			accounts = testsuite.MustSucceed(signer.ExecutionAccounts(ctx))
			gomega.Expect(accounts).NotTo(gomega.BeEmpty(), "the signer holds no account")
			addresses := make([]common.Address, len(accounts))
			descriptor := testsuite.MustSucceed(walletmldsa87.NewMLDSA87Descriptor())
			for index, account := range accounts {
				addresses[index] = testsuite.MustSucceed(common.NewAddressFromString(account.Address))
				// An address is derived from its public key, which is what a
				// verifier of the account's signatures needs.
				key := testsuite.MustSucceed(walletmldsa87.BytesToPK(testsuite.MustSucceed(hexutil.Decode(account.PublicKey))))
				derived := testsuite.MustSucceed(walletmldsa87.GetMLDSA87Address(key, descriptor))
				gomega.Expect(common.Address(derived)).To(gomega.Equal(addresses[index]),
					"the public key of %s derives another address", account.Address)
				balance := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, addresses[index], nil))
				gomega.Expect(balance.Sign()).To(gomega.BeNumerically(">", 0), "account %s is not funded", account.Address)
			}
			gomega.Expect(slices.IsSortedFunc(addresses, func(a, b common.Address) int { return a.Cmp(b) })).
				To(gomega.BeTrue(), "the signer lists its accounts unsorted")
			from = addresses[0]

			// qrl_accounts lists the same accounts, which the signer answers
			// itself.
			var listed []common.Address
			gomega.Expect(call(ctx, &listed, "qrl_accounts")).To(gomega.Succeed())
			gomega.Expect(listed).To(gomega.Equal(addresses))
			gomega.Expect(sum(scrape(ctx), remotesigner.MetricSignersLoaded, nil)).To(gomega.Equal(float64(len(accounts))))

			health := testsuite.MustSucceed(signer.Healthcheck(ctx))
			gomega.Expect(health.Status).To(gomega.Equal("UP"))
			gomega.Expect(health.Checks).To(gomega.ContainElement(gomega.HaveField("ID", "keys-check/config-files-loading")))
			for _, check := range health.Checks {
				gomega.Expect(check.Status).To(gomega.Equal("UP"), "healthcheck %s", check.ID)
			}

			for name, client := range map[string]*remotesigner.Client{
				"no token":      signer.WithToken(""),
				"a wrong token": signer.WithToken("0x" + strings.Repeat("0", 64)),
			} {
				_, err := client.ExecutionAccounts(ctx)
				refusal, ok := remotesigner.AsResponseError(err)
				gomega.Expect(ok).To(gomega.BeTrue(), "publicKeys with %s: %v", name, err)
				gomega.Expect(refusal.StatusCode).To(gomega.Equal(http.StatusUnauthorized), "publicKeys with %s", name)

				anonymous := testsuite.MustSucceed(client.DialRPC(ctx))
				err = anonymous.CallContext(ctx, new([]common.Address), "qrl_accounts")
				anonymous.Close()
				var httpErr rpc.HTTPError
				gomega.Expect(errors.As(err, &httpErr)).To(gomega.BeTrue(), "qrl_accounts with %s: %v", name, err)
				gomega.Expect(httpErr.StatusCode).To(gomega.Equal(http.StatusUnauthorized), "qrl_accounts with %s", name)
			}
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("sends a value transfer that is mined", func(ctx ginkgo.SpecContext) {
			value := big.NewInt(params.Quanta)
			before := testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, nil))
			nonce := pendingNonce(ctx)
			sendsBefore := served(ctx, "qrl_sendTransaction")
			rawSendsBefore := served(ctx, "qrl_sendRawTransaction")

			// The signer asks the execution client for the nonce, the gas and
			// the fees, signs, and sends the transaction to it.
			gomega.Expect(call(ctx, &sentHash, "qrl_sendTransaction", map[string]any{
				"from": from, "to": recipient, "value": (*hexutil.Big)(value),
			})).To(gomega.Succeed())
			receipt := waitMined(ctx, sentHash)
			gomega.Expect(receipt.Status).To(gomega.Equal(types.ReceiptStatusSuccessful))

			tx, pending, err := node.Execution.TransactionByHash(ctx, sentHash)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(pending).To(gomega.BeFalse())
			gomega.Expect(types.Sender(txSigner, tx)).To(gomega.Equal(from))
			gomega.Expect(tx.ChainId()).To(gomega.Equal(node.ChainID))
			gomega.Expect(tx.To()).To(gomega.HaveValue(gomega.Equal(recipient)))
			gomega.Expect(tx.Value()).To(gomega.Equal(value))
			gomega.Expect(tx.Nonce()).To(gomega.Equal(nonce))
			gomega.Expect(new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap()).Cmp(feeCap)).
				To(gomega.BeNumerically("<=", 0), "the fees the execution client filled in are above the cap")
			gomega.Expect(testsuite.MustSucceed(node.Execution.BalanceAt(ctx, recipient, receipt.BlockNumber))).
				To(gomega.Equal(new(big.Int).Add(before, value)))
			// The execution client got the signed transaction, and was never
			// asked to sign one.
			gomega.Expect(served(ctx, "qrl_sendRawTransaction")).To(gomega.BeNumerically(">", rawSendsBefore))
			gomega.Expect(served(ctx, "qrl_sendTransaction")).To(gomega.Equal(sendsBefore))
		}, ginkgo.SpecTimeout(2*time.Minute))

		ginkgo.It("signs a transaction that is mined once its client sends it", func(ctx ginkgo.SpecContext) {
			nonce := pendingNonce(ctx)
			var signed struct {
				Raw hexutil.Bytes `json:"raw"`
				Tx  struct {
					Hash common.Hash `json:"hash"`
				} `json:"tx"`
			}
			gomega.Expect(call(ctx, &signed, "qrl_signTransaction", transaction(ctx, nonce))).To(gomega.Succeed())
			tx := new(types.Transaction)
			gomega.Expect(tx.UnmarshalBinary(signed.Raw)).To(gomega.Succeed())
			signedHash = tx.Hash()
			gomega.Expect(signedHash).To(gomega.Equal(signed.Tx.Hash))
			gomega.Expect(types.Sender(txSigner, tx)).To(gomega.Equal(from))
			gomega.Expect(tx.Nonce()).To(gomega.Equal(nonce))
			// The signer never sends what qrl_signTransaction signs.
			gomega.Expect(pendingNonce(ctx)).To(gomega.Equal(nonce))

			// The client sends it through the signer, which forwards
			// qrl_sendRawTransaction.
			gomega.Expect(proxied.SendTransaction(ctx, tx)).To(gomega.Succeed())
			gomega.Expect(waitMined(ctx, signedHash).Status).To(gomega.Equal(types.ReceiptStatusSuccessful))
		}, ginkgo.SpecTimeout(2*time.Minute))

		ginkgo.It("signs messages whose signatures verify with go-qrllib", func(ctx ginkgo.SpecContext) {
			descriptor := testsuite.MustSucceed(walletmldsa87.NewMLDSA87Descriptor())
			keys := make([]walletmldsa87.PK, len(accounts))
			for index, account := range accounts {
				keys[index] = testsuite.MustSucceed(walletmldsa87.BytesToPK(testsuite.MustSucceed(hexutil.Decode(account.PublicKey))))
			}
			data := []byte(message)
			for index, account := range accounts {
				var signature hexutil.Bytes
				gomega.Expect(call(ctx, &signature, "qrl_sign", account.Address, hexutil.Bytes(data))).To(gomega.Succeed())
				gomega.Expect(signature).To(gomega.HaveLen(walletmldsa87.SigSize))
				gomega.Expect(walletmldsa87.Verify(qrlaccounts.TextHash(data), signature, &keys[index], [3]byte(descriptor))).
					To(gomega.BeTrue(), "the signature of %s does not verify", account.Address)
				gomega.Expect(walletmldsa87.Verify(qrlaccounts.TextHash([]byte(message+"!")), signature, &keys[index], [3]byte(descriptor))).
					To(gomega.BeFalse(), "the signature of %s verifies another message", account.Address)
				other := keys[(index+1)%len(keys)]
				if len(keys) > 1 {
					gomega.Expect(walletmldsa87.Verify(qrlaccounts.TextHash(data), signature, &other, [3]byte(descriptor))).
						To(gomega.BeFalse(), "the signature of %s verifies under another account's key", account.Address)
				}
			}

			// An account the signer does not hold signs nothing.
			err := call(ctx, new(hexutil.Bytes), "qrl_sign", recipient, hexutil.Bytes(data))
			var rpcErr rpc.Error
			gomega.Expect(errors.As(err, &rpcErr)).To(gomega.BeTrue(), "qrl_sign for an account it does not hold: %v", err)
			gomega.Expect(rpcErr.ErrorCode()).To(gomega.Equal(-32000))
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("refuses a transaction for another chain", func(ctx ginkgo.SpecContext) {
			nonce := pendingNonce(ctx)
			for _, method := range []string{"qrl_sendTransaction", "qrl_signTransaction"} {
				tx := transaction(ctx, nonce)
				tx["chainId"] = (*hexutil.Big)(big.NewInt(wrongChainID))
				expectRejected(call(ctx, nil, method, tx), fmt.Sprintf("chainId %d is not --chain-id %s", wrongChainID, node.ChainID))
			}
			gomega.Expect(pendingNonce(ctx)).To(gomega.Equal(nonce), "a refused transaction reached the pool")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("refuses a transaction whose fee is above its cap", func(ctx ginkgo.SpecContext) {
			nonce := pendingNonce(ctx)
			for _, method := range []string{"qrl_sendTransaction", "qrl_signTransaction"} {
				tx := transaction(ctx, nonce)
				tx["gas"], tx["maxFeePerGas"] = overCapGas, overCapFeePerGas
				expectRejected(call(ctx, nil, method, tx), "is above the cap of "+feeCap.String()+" Planck")
			}
			gomega.Expect(pendingNonce(ctx)).To(gomega.Equal(nonce), "a refused transaction reached the pool")
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("forwards net_version and refuses the admin, debug and personal namespaces", func(ctx ginkgo.SpecContext) {
			var forwarded, direct string
			before := served(ctx, forwardedMethod)
			gomega.Expect(call(ctx, &forwarded, forwardedMethod)).To(gomega.Succeed())
			gomega.Expect(served(ctx, forwardedMethod)).To(gomega.BeNumerically(">", before),
				"the execution client did not serve the forwarded %s", forwardedMethod)
			gomega.Expect(node.Execution.Client().CallContext(ctx, &direct, forwardedMethod)).To(gomega.Succeed())
			gomega.Expect(forwarded).To(gomega.Equal(direct))
			gomega.Expect(forwarded).To(gomega.Equal(node.ChainID.String()))

			for _, refused := range refusedMethods {
				before := served(ctx, refused.method)
				err := call(ctx, nil, refused.method, refused.params...)
				var rpcErr rpc.Error
				gomega.Expect(errors.As(err, &rpcErr)).To(gomega.BeTrue(), "the signer answered %s: %v", refused.method, err)
				gomega.Expect(rpcErr.ErrorCode()).To(gomega.Equal(remotesigner.CodeMethodNotFound), "%s: %v", refused.method, err)
				gomega.Expect(rpcErr.Error()).To(gomega.Equal(remotesigner.MessageMethodNotFound), "%s", refused.method)

				// The refusal is the signer's: the execution client answers
				// the method, or refuses it in words of its own.
				err = node.Execution.Client().CallContext(ctx, new(any), refused.method, refused.params...)
				if refused.node {
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "the execution client does not answer %s", refused.method)
					// It counted the direct call, and no call before it: the
					// signer's never reached it.
					gomega.Expect(served(ctx, refused.method)).To(gomega.Equal(before+1),
						"calls of %s the execution client served", refused.method)
				} else {
					gomega.Expect(err).To(gomega.HaveOccurred(), "the execution client answers %s", refused.method)
					gomega.Expect(err.Error()).NotTo(gomega.Equal(remotesigner.MessageMethodNotFound))
				}
			}
		}, ginkgo.SpecTimeout(time.Minute))

		ginkgo.It("records what it signed, sent and refused in its audit log and metrics", func(ctx ginkgo.SpecContext) {
			gomega.Expect(sentHash).NotTo(gomega.BeZero(), "no transaction was sent through the signer")
			gomega.Expect(signedHash).NotTo(gomega.BeZero(), "no transaction was signed by the signer")
			// The entries of the specs' requests: the log is append-only.
			entries := auditLog(ctx)
			gomega.Expect(len(entries)).To(gomega.BeNumerically(">=", initialAuditEntries), "the audit log lost entries")
			entries = entries[initialAuditEntries:]
			chainID := hexutil.EncodeBig(node.ChainID)

			outcomes := func(hash common.Hash) []string {
				var found []string
				for _, entry := range entries {
					if strings.EqualFold(entry.Hash, hash.Hex()) {
						gomega.Expect(strings.EqualFold(entry.From, from.Hex())).To(gomega.BeTrue(), "entry %+v", entry)
						gomega.Expect(strings.EqualFold(entry.To, recipient.Hex())).To(gomega.BeTrue(), "entry %+v", entry)
						gomega.Expect(entry.ChainID).To(gomega.Equal(chainID), "entry %+v", entry)
						found = append(found, entry.Method+" "+entry.Outcome)
					}
				}
				return found
			}
			gomega.Expect(outcomes(sentHash)).To(gomega.Equal([]string{
				"qrl_sendTransaction " + remotesigner.AuditSigned, "qrl_sendTransaction " + remotesigner.AuditSent,
			}))
			// The signer records what qrl_signTransaction returned, not what
			// the client did with it.
			gomega.Expect(outcomes(signedHash)).To(gomega.Equal([]string{"qrl_signTransaction " + remotesigner.AuditSigned}))

			var refusals []string
			for _, entry := range entries {
				gomega.Expect(entry.Outcome).NotTo(gomega.Equal(remotesigner.AuditNodeRefused),
					"the execution client refused a transaction: %+v", entry)
				if entry.Outcome == remotesigner.AuditRefused {
					reason := "the chain ID"
					if strings.Contains(entry.Reason, "above the cap") {
						reason = "the fee cap"
					} else {
						gomega.Expect(entry.Reason).To(gomega.ContainSubstring("is not --chain-id"), "entry %+v", entry)
					}
					refusals = append(refusals, entry.Method+" "+reason)
				}
			}
			gomega.Expect(refusals).To(gomega.ConsistOf(
				"qrl_sendTransaction the chain ID", "qrl_signTransaction the chain ID",
				"qrl_sendTransaction the fee cap", "qrl_signTransaction the fee cap",
			))

			// Every call above was answered by the JSON-RPC route with 200:
			// the refusals of a transaction or a method too.
			final := scrape(ctx)
			answered := remotesigner.With("route", remotesigner.ExecutionRPCRoute, "code", "200")
			gomega.Expect(sum(final, remotesigner.MetricHTTPRequestDurations, answered)-
				sum(initial, remotesigner.MetricHTTPRequestDurations, answered)).
				To(gomega.BeNumerically(">=", calls), "JSON-RPC requests answered with 200")
			listed := remotesigner.With("route", "/api/v1/execution/publicKeys", "code", "200")
			gomega.Expect(sum(final, remotesigner.MetricHTTPRequestDurations, listed)).
				To(gomega.BeNumerically(">", sum(initial, remotesigner.MetricHTTPRequestDurations, listed)))
			gomega.Expect(sum(final, remotesigner.MetricHTTPRequestDurations, remotesigner.ServerFaults())).
				To(gomega.BeZero(), "API requests answered with a 5xx")
			gomega.Expect(sum(final, remotesigner.MetricSignersLoaded, nil)).To(gomega.Equal(float64(len(accounts))))
		}, ginkgo.SpecTimeout(time.Minute))
	},
)

// servedCalls reads gqrl's metrics at metricsURL for the calls of method it
// served: its rpc_duration_<method>_success and _failure timers, whose counts
// it exports as rpc_duration_<method>_success_count and _failure_count. A
// method it never served has neither.
func servedCalls(ctx context.Context, metricsURL, method string) (float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL+devnet.ExecutionMetricsPath, nil)
	if err != nil {
		return 0, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, err
	}
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s returned %d", devnet.ExecutionMetricsPath, response.StatusCode)
	}
	var calls float64
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		for _, outcome := range []string{"success", "failure"} {
			value, ok := strings.CutPrefix(scanner.Text(), "rpc_duration_"+method+"_"+outcome+"_count ")
			if !ok {
				continue
			}
			count, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, fmt.Errorf("%s: %w", scanner.Text(), err)
			}
			calls += count
		}
	}
	return calls, scanner.Err()
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// slug names a spec's diagnostics directory.
func slug(text string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(text), "-"), "-")
}
