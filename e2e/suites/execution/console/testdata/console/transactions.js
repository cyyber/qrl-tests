var suite = createConsoleSuite("transactions");
var check = suite.check;

loadScript(".params.js");

check("node-managed signer transfers value between accounts", function () {
    var managed = qrl.accounts;
    if (!(managed instanceof Array) || managed.length === 0) {
        throw new Error("unexpected node-managed accounts: " + JSON.stringify(managed));
    }

    var sender = web3.toChecksumAddress(managed[0]);
    var recipient = web3.toChecksumAddress(PARAMS.transferRecipient);
    var value = web3.toPlanck("1", "shor");
    var balanceBefore = qrl.getBalance(recipient);
    var txHash = qrl.sendTransaction({
        from: sender,
        to: recipient,
        value: value
    });
    requireHash("value transfer transaction hash", txHash);

    var receipt = waitForReceipt(txHash);
    if (Number(receipt.status) !== 1 ||
        receipt.transactionHash !== txHash ||
        web3.toChecksumAddress(receipt.from) !== sender ||
        web3.toChecksumAddress(receipt.to) !== recipient ||
        receipt.contractAddress !== null) {
        throw new Error("value transfer failed: " + JSON.stringify(receipt));
    }

    var transaction = qrl.getTransaction(txHash);
    if (transaction === null ||
        web3.toChecksumAddress(transaction.from) !== sender ||
        web3.toChecksumAddress(transaction.to) !== recipient ||
        transaction.value.toString(10) !== value ||
        transaction.input !== "0x") {
        throw new Error("unexpected value transfer: " + JSON.stringify(transaction));
    }

    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.plus(value).toString(10)) {
        throw new Error("recipient balance did not increase by " + value);
    }
});

suite.finish();
