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

function higherFee(first, second) {
    return first.gt(second) ? first : second;
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

function requirePooled(description, transactionHash) {
    var transaction = qrl.getTransaction(transactionHash);
    if (transaction === null) {
        throw new Error(description + " is not in the pool");
    }
    if (transaction.blockNumber !== null) {
        throw new Error(description + " was mined");
    }
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

// A congested network raises the base fee above a waiting transaction's fee
// cap, and no block can include the transaction until its sender raises the
// fees. The devnet cannot be congested on demand, so the check sends a
// transaction whose fee cap starts below the base fee and then replaces it.
check("node-managed signer raises the fees of a stuck transaction", function () {
    var sender = web3.toChecksumAddress(qrl.accounts[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a6"));
    requireAddress("replacement recipient", recipient);

    var baseFee = qrl.getBlock("latest").baseFeePerGas;
    // The base fee falls by an eighth per empty block, so a 32nd of it stays
    // below it for 25 blocks, even after a price bump. It stops falling at 7
    // planck, which is above 4 planck after a price bump.
    var stuckFeeCap = higherFee(baseFee.dividedToIntegerBy(32), web3.toBigNumber(4));
    if (!bumpFee(stuckFeeCap).lt(baseFee)) {
        throw new Error("the base fee " + baseFee + " leaves no room for a stuck fee cap");
    }
    // Half the fee cap, so a bumped tip stays below the fee cap and the
    // replacement is rejected as underpriced, not as invalid.
    var stuckTip = stuckFeeCap.dividedToIntegerBy(2);
    // The raised tip is the suggested one, unless the price bump over the
    // bumped tip asks for more. The fee cap has room for the base fee to
    // double.
    var raisedTip = higherFee(qrl.maxPriorityFeePerGas, bumpFee(bumpFee(stuckTip)));
    var raisedFeeCap = baseFee.times(2).plus(raisedTip);

    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);
    var transfer = function (value, maxFeePerGas, maxPriorityFeePerGas) {
        return {
            from: sender,
            to: recipient,
            value: value,
            nonce: nonce,
            // The node cannot estimate the gas of a transaction whose fee cap
            // is below the base fee.
            gas: 21000,
            maxFeePerGas: maxFeePerGas,
            maxPriorityFeePerGas: maxPriorityFeePerGas
        };
    };

    var stuck = qrl.sendTransaction(transfer(1, stuckFeeCap, stuckTip));
    var bumped = null;
    var failure = null;
    try {
        requireHash("stuck transaction hash", stuck);
        // A transaction that pays the base fee is mined within two blocks.
        waitForBlocks(3);
        requirePooled("stuck transaction", stuck);
        // The pending nonce moved: the transaction has the sender's next
        // nonce, and only its fees keep it out of a block.
        if (qrl.getTransactionCount(sender, "pending") !== nonce + 1) {
            throw new Error("stuck transaction is not executable");
        }

        // Below 10 planck the price bump rounds down to nothing and the pool
        // only asks for a higher fee. On a chain whose base fee has settled,
        // the stuck fees are that low, and the "one planck short" cases
        // repeat the two before them.
        requireUnderpriced("replacement with the same fees",
            transfer(2, stuckFeeCap, stuckTip));
        requireUnderpriced("replacement with only the fee cap raised",
            transfer(2, bumpFee(stuckFeeCap), stuckTip));
        requireUnderpriced("replacement with only the tip raised",
            transfer(2, stuckFeeCap, bumpFee(stuckTip)));
        requireUnderpriced("replacement with the fee cap one planck short of the price bump",
            transfer(2, bumpFee(stuckFeeCap).minus(1), bumpFee(stuckTip)));
        requireUnderpriced("replacement with the tip one planck short of the price bump",
            transfer(2, bumpFee(stuckFeeCap), bumpFee(stuckTip).minus(1)));
        requirePooled("stuck transaction", stuck);

        // Raising both fees by the price bump replaces the transaction, but
        // the fee cap is still below the base fee.
        bumped = qrl.sendTransaction(transfer(2, bumpFee(stuckFeeCap), bumpFee(stuckTip)));
        requireHash("bumped transaction hash", bumped);
        if (qrl.getTransaction(stuck) !== null) {
            throw new Error("stuck transaction is still in the pool");
        }
        requirePooled("bumped transaction", bumped);
    } catch (error) {
        failure = error;
    }

    // The raised transaction is sent even after a failure: a stuck transaction
    // blocks every later transaction of the account.
    var raised;
    try {
        raised = qrl.sendTransaction(transfer(3, raisedFeeCap, raisedTip));
    } catch (error) {
        throw failure !== null ? failure : error;
    }
    if (failure !== null) {
        throw failure;
    }
    requireHash("raised transaction hash", raised);

    var receipt = waitForReceipt(raised);
    if (Number(receipt.status) !== 1) {
        throw new Error("raised transaction failed: " + JSON.stringify(receipt));
    }
    if (qrl.getTransactionReceipt(stuck) !== null) {
        throw new Error("stuck transaction was mined");
    }
    if (qrl.getTransactionReceipt(bumped) !== null) {
        throw new Error("bumped transaction was mined");
    }
    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.plus(3).toString(10)) {
        throw new Error("recipient received " + balanceAfter.minus(balanceBefore) +
            ", want the raised transaction's value 3");
    }
});

suite.finish();
