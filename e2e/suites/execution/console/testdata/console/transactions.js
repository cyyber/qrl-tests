var suite = createConsoleSuite("transactions");
var check = suite.check;

check("node-managed signer transfers value between accounts", function () {
    var managed = qrl.accounts;
    if (!(managed instanceof Array) || managed.length === 0) {
        throw new Error("unexpected node-managed accounts: " + JSON.stringify(managed));
    }

    var sender = web3.toChecksumAddress(managed[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a5"));
    requireAddress("transfer recipient", recipient);
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

// The pool's default --txpool.pricebump: a replacement must raise both fees by
// this percentage.
var priceBump = 10;

function bumpFee(fee) {
    var bumped = fee.times(100 + priceBump).dividedToIntegerBy(100);
    return bumped.gt(fee) ? bumped : fee.plus(1);
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

check("node-managed signer replaces a pending transaction", function () {
    var sender = web3.toChecksumAddress(qrl.accounts[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a6"));
    requireAddress("replacement recipient", recipient);
    var tip = qrl.maxPriorityFeePerGas;
    // Twice the gas price leaves room for a bumped tip below the fee cap.
    var feeCap = qrl.gasPrice.times(2);
    var transfer = function (transactionNonce, value, maxFeePerGas, maxPriorityFeePerGas) {
        return {
            from: sender,
            to: recipient,
            value: value,
            nonce: transactionNonce,
            maxFeePerGas: maxFeePerGas,
            maxPriorityFeePerGas: maxPriorityFeePerGas
        };
    };

    // The next nonce is left free, so the transaction waits in the pool
    // instead of being included in the next block.
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);
    var original = qrl.sendTransaction(transfer(nonce + 1, 1, feeCap, tip));
    requireHash("original transaction hash", original);
    if (qrl.getTransaction(original) === null) {
        throw new Error("original transaction is not in the pool");
    }
    if (qrl.getTransactionCount(sender, "pending") !== nonce) {
        throw new Error("original transaction is not queued behind the nonce gap");
    }

    requireUnderpriced("replacement with the same fees",
        transfer(nonce + 1, 2, feeCap, tip));
    requireUnderpriced("replacement with only the fee cap raised",
        transfer(nonce + 1, 2, bumpFee(feeCap), tip));
    requireUnderpriced("replacement with only the tip raised",
        transfer(nonce + 1, 2, feeCap, bumpFee(tip)));
    requireUnderpriced("replacement with the fee cap one planck short of the price bump",
        transfer(nonce + 1, 2, bumpFee(feeCap).minus(1), bumpFee(tip)));
    requireUnderpriced("replacement with the tip one planck short of the price bump",
        transfer(nonce + 1, 2, bumpFee(feeCap), bumpFee(tip).minus(1)));

    var replacement = qrl.sendTransaction(transfer(nonce + 1, 2, bumpFee(feeCap), bumpFee(tip)));
    requireHash("replacement transaction hash", replacement);
    if (qrl.getTransaction(original) !== null) {
        throw new Error("original transaction is still in the pool");
    }

    // Fill the nonce gap, so the replacement becomes executable.
    requireHash("gap transaction hash", qrl.sendTransaction(transfer(nonce, 0, feeCap, tip)));
    var receipt = waitForReceipt(replacement);
    if (Number(receipt.status) !== 1) {
        throw new Error("replacement failed: " + JSON.stringify(receipt));
    }
    if (qrl.getTransactionReceipt(original) !== null) {
        throw new Error("original transaction was mined");
    }
    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.plus(2).toString(10)) {
        throw new Error("recipient received " + balanceAfter.minus(balanceBefore) +
            ", want the replacement's value 2");
    }
});

suite.finish();
