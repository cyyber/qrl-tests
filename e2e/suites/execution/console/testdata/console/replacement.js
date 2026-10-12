var suite = createConsoleSuite("replacement");
var check = suite.check;

// A plain transfer's gas. It is passed explicitly, because the node cannot
// estimate the gas of a transaction whose fee cap is below the base fee.
var transferGas = 21000;

// Fees of a stuck transaction, in planck. The base fee never drops below 7,
// so no block includes it. The tip is lower, so it can be raised on its own.
var stuckFeeCap = web3.toBigNumber(4);
var stuckTip = web3.toBigNumber(2);

// Normal fees, which cover the base fee. Twice the gas price, which includes
// the tip, leaves room for the base fee to rise.
var tip = qrl.maxPriorityFeePerGas;
var feeCap = qrl.gasPrice.times(2);

var sender = web3.toChecksumAddress(qrl.accounts[0]);
var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a6"));
requireAddress("recipient", recipient);

// payment sends one planck from the sender.
function payment(to, nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return {
        from: sender,
        to: to,
        value: 1,
        nonce: nonce,
        gas: transferGas,
        maxFeePerGas: maxFeePerGas,
        maxPriorityFeePerGas: maxPriorityFeePerGas
    };
}

function requireUnderpriced(description, transaction) {
    try {
        qrl.sendTransaction(transaction);
    } catch (error) {
        if (String(error).indexOf("replacement transaction underpriced") === -1) {
            throw new Error(description + " failed with: " + error);
        }
        return;
    }
    throw new Error(description + " was accepted");
}

function requireInPool(transactionHash) {
    var transaction = qrl.getTransaction(transactionHash);
    if (transaction === null) {
        throw new Error("transaction is not in the pool");
    }
    if (transaction.blockNumber !== null) {
        throw new Error("transaction was mined");
    }
}

// sendDespite sends a transaction even after an earlier failure, and then
// rethrows the failure: a stuck transaction blocks every later transaction of
// its account, so the one that replaces it has to be sent.
function sendDespite(failure, transaction) {
    var transactionHash;
    try {
        transactionHash = qrl.sendTransaction(transaction);
    } catch (error) {
        throw failure !== null ? failure : error;
    }
    if (failure !== null) {
        throw failure;
    }
    return transactionHash;
}

function waitForBlocks(count) {
    var target = qrl.blockNumber + count;
    for (var attempt = 0; attempt < 60; attempt++) {
        if (qrl.blockNumber >= target) {
            return;
        }
        admin.sleep(1);
    }
    throw new Error("chain did not advance " + count + " blocks within timeout");
}

// On a congested network a waiting transaction is first outbid by higher
// tips, and then left behind for good once the base fee rises above its fee
// cap. This simulates the second: the transaction starts with a fee cap below
// the base fee.
check("node-managed signer raises the fees of a stuck transaction", function () {
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);

    var stuck = qrl.sendTransaction(payment(recipient, nonce, stuckFeeCap, stuckTip));
    var failure = null;
    try {
        // Normal fees take at most two blocks.
        waitForBlocks(3);
        requireInPool(stuck);
        if (qrl.getTransactionCount(sender, "pending") !== nonce + 1) {
            throw new Error("stuck transaction is not executable");
        }

        // Another recipient, so the same fees do not make the same
        // transaction.
        requireUnderpriced("replacement transaction with the same fees",
            payment(sender, nonce, stuckFeeCap, stuckTip));
        requireUnderpriced("replacement transaction with only the fee cap raised",
            payment(sender, nonce, feeCap, stuckTip));
        requireUnderpriced("replacement transaction with only the tip raised",
            payment(sender, nonce, stuckFeeCap, stuckFeeCap));
        requireInPool(stuck);
    } catch (error) {
        failure = error;
    }

    // The stuck transaction's payment again, with fees that cover the base
    // fee.
    var raised = sendDespite(failure, payment(recipient, nonce, feeCap, tip));
    var receipt = waitForReceipt(raised);
    if (Number(receipt.status) !== 1) {
        throw new Error("raised transaction failed: " + JSON.stringify(receipt));
    }
    if (qrl.getTransactionReceipt(stuck) !== null) {
        throw new Error("stuck transaction was mined");
    }
    var received = qrl.getBalance(recipient).minus(balanceBefore);
    if (received.toString(10) !== "1") {
        throw new Error("recipient received " + received + ", want 1");
    }
});

suite.finish();
