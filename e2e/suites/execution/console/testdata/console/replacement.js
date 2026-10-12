var suite = createConsoleSuite("replacement");
var check = suite.check;

// The pool's default --txpool.pricebump: a replacement must raise both fees by
// this percentage.
var priceBump = 10;

// A plain transfer's gas. It is passed explicitly, because the node cannot
// estimate the gas of a transaction whose fee cap is below the base fee.
var transferGas = 21000;

function bumpFee(fee) {
    var bumped = fee.times(100 + priceBump).dividedToIntegerBy(100);
    return bumped.gt(fee) ? bumped : fee.plus(1);
}

function higherFee(first, second) {
    return first.gt(second) ? first : second;
}

// replacementFees returns stuck fees, which are below the base fee and keep a
// transaction out of blocks, and raised fees, which cover it.
function replacementFees() {
    var baseFee = qrl.getBlock("latest").baseFeePerGas;
    // The stuck fee cap has to stay below the base fee until the checks have
    // replaced the transaction. On an idle chain the base fee drops by 1/8
    // with every block, so the cap is 1/32 of it: the base fee takes about 25
    // blocks to drop that far.
    //
    // On a chain that has been idle for long, the base fee has dropped to 7
    // planck and stays there, and 1/32 of it is zero. The cap is then 4
    // planck, which is below 7, and so is the 5 planck that a check raises it
    // to.
    var stuckFeeCap = higherFee(baseFee.dividedToIntegerBy(32), web3.toBigNumber(4));
    if (!bumpFee(stuckFeeCap).lt(baseFee)) {
        throw new Error("the base fee " + baseFee + " leaves no room for a stuck fee cap");
    }
    // Half the fee cap, so a bumped tip stays below the fee cap and the
    // replacement is rejected as underpriced, not as invalid.
    var stuckTip = stuckFeeCap.dividedToIntegerBy(2);
    // The raised tip is the suggested one, unless the price bump over a
    // bumped tip asks for more. The fee cap has room for the base fee to
    // double.
    var raisedTip = higherFee(qrl.maxPriorityFeePerGas, bumpFee(bumpFee(stuckTip)));
    return {
        stuckFeeCap: stuckFeeCap,
        stuckTip: stuckTip,
        raisedFeeCap: baseFee.times(2).plus(raisedTip),
        raisedTip: raisedTip
    };
}

function payment(account, recipient, value, nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return {
        from: account,
        to: recipient,
        value: value,
        nonce: nonce,
        gas: transferGas,
        maxFeePerGas: maxFeePerGas,
        maxPriorityFeePerGas: maxPriorityFeePerGas
    };
}

// cancellation is a transfer of nothing from an account to itself: it takes a
// nonce and does nothing else.
function cancellation(account, nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return payment(account, account, 0, nonce, maxFeePerGas, maxPriorityFeePerGas);
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

function requireReplaced(description, transactionHash) {
    if (qrl.getTransaction(transactionHash) !== null) {
        throw new Error(description + " is still in the pool");
    }
}

function requireUnmined(description, transactionHash) {
    if (qrl.getTransactionReceipt(transactionHash) !== null) {
        throw new Error(description + " was mined");
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

// A congested network raises the base fee above a waiting transaction's fee
// cap, and no block can include the transaction until its sender raises the
// fees. The devnet cannot be congested on demand, so the check sends a
// transaction whose fee cap starts below the base fee and then replaces it.
check("node-managed signer raises the fees of a stuck transaction", function () {
    var sender = web3.toChecksumAddress(qrl.accounts[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a6"));
    requireAddress("stuck transaction recipient", recipient);
    var fees = replacementFees();
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);
    var transfer = function (value, maxFeePerGas, maxPriorityFeePerGas) {
        return payment(sender, recipient, value, nonce, maxFeePerGas, maxPriorityFeePerGas);
    };

    var stuck = qrl.sendTransaction(transfer(1, fees.stuckFeeCap, fees.stuckTip));
    var bumped = null;
    var failure = null;
    try {
        requireHash("stuck transaction hash", stuck);
        // A transaction with normal fees is mined within two blocks, so one
        // left out of three is stuck, not slow.
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
        // repeat the two before them. The last check has them with fees that
        // are always large enough.
        requireUnderpriced("replacement with the same fees",
            transfer(2, fees.stuckFeeCap, fees.stuckTip));
        requireUnderpriced("replacement with only the fee cap raised",
            transfer(2, bumpFee(fees.stuckFeeCap), fees.stuckTip));
        requireUnderpriced("replacement with only the tip raised",
            transfer(2, fees.stuckFeeCap, bumpFee(fees.stuckTip)));
        requireUnderpriced("replacement with the fee cap one planck short of the price bump",
            transfer(2, bumpFee(fees.stuckFeeCap).minus(1), bumpFee(fees.stuckTip)));
        requireUnderpriced("replacement with the tip one planck short of the price bump",
            transfer(2, bumpFee(fees.stuckFeeCap), bumpFee(fees.stuckTip).minus(1)));
        requirePooled("stuck transaction", stuck);

        // Raising both fees by the price bump replaces the transaction, but
        // the fee cap is still below the base fee.
        bumped = qrl.sendTransaction(transfer(2, bumpFee(fees.stuckFeeCap), bumpFee(fees.stuckTip)));
        requireHash("bumped transaction hash", bumped);
        requireReplaced("stuck transaction", stuck);
        requirePooled("bumped transaction", bumped);
    } catch (error) {
        failure = error;
    }

    // The stuck transaction's payment again, with fees that cover the base
    // fee.
    var raised = sendDespite(failure, transfer(1, fees.raisedFeeCap, fees.raisedTip));
    requireHash("raised transaction hash", raised);

    var receipt = waitForReceipt(raised);
    if (Number(receipt.status) !== 1) {
        throw new Error("raised transaction failed: " + JSON.stringify(receipt));
    }
    requireUnmined("stuck transaction", stuck);
    requireUnmined("bumped transaction", bumped);
    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.plus(1).toString(10)) {
        throw new Error("recipient received " + balanceAfter.minus(balanceBefore) +
            ", want the payment's value 1");
    }
});

// A transaction sent by mistake is cancelled the same way: a transfer of
// nothing to its own sender takes its nonce, so it can never be mined. The
// mistaken transaction has the stuck fees, so no block includes it before it
// is replaced.
check("node-managed signer cancels a stuck transaction with a zero-value transfer to itself", function () {
    var sender = web3.toChecksumAddress(qrl.accounts[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a7"));
    requireAddress("mistaken transaction recipient", recipient);
    var fees = replacementFees();
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);

    var mistaken = qrl.sendTransaction(payment(sender, recipient, 1, nonce, fees.stuckFeeCap, fees.stuckTip));
    var failure = null;
    try {
        requireHash("mistaken transaction hash", mistaken);
        requirePooled("mistaken transaction", mistaken);
    } catch (error) {
        failure = error;
    }

    var cancel = sendDespite(failure, cancellation(sender, nonce, fees.raisedFeeCap, fees.raisedTip));
    requireHash("cancel transaction hash", cancel);
    requireReplaced("mistaken transaction", mistaken);

    var receipt = waitForReceipt(cancel);
    if (Number(receipt.status) !== 1) {
        throw new Error("cancel transaction failed: " + JSON.stringify(receipt));
    }
    requireUnmined("mistaken transaction", mistaken);
    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.toString(10)) {
        throw new Error("mistaken transaction's recipient received " + balanceAfter.minus(balanceBefore));
    }
});

// A transaction sent one nonce ahead waits in the pool's queue, where no block
// can include it, until the gap is filled. Its sender cancels it there, and
// the cancelling transfer is mined once the gap is filled.
check("node-managed signer cancels a transaction sent one nonce ahead", function () {
    var sender = web3.toChecksumAddress(qrl.accounts[0]);
    var recipient = web3.toChecksumAddress("Q" + new Array(65).join("a8"));
    requireAddress("mistaken transaction recipient", recipient);
    var fees = replacementFees();
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(recipient);

    var mistaken = qrl.sendTransaction(
        payment(sender, recipient, 1, nonce + 1, fees.raisedFeeCap, fees.raisedTip));
    requireHash("mistaken transaction hash", mistaken);
    requirePooled("mistaken transaction", mistaken);
    if (qrl.getTransactionCount(sender, "pending") !== nonce) {
        throw new Error("mistaken transaction is not queued behind the nonce gap");
    }

    // The raised fees are far above 10 planck, so the "one planck short" cases
    // pin the price bump whatever the base fee is.
    requireUnderpriced("cancelling transfer with the same fees",
        cancellation(sender, nonce + 1, fees.raisedFeeCap, fees.raisedTip));
    requireUnderpriced("cancelling transfer with the fee cap one planck short of the price bump",
        cancellation(sender, nonce + 1, bumpFee(fees.raisedFeeCap).minus(1), bumpFee(fees.raisedTip)));
    requireUnderpriced("cancelling transfer with the tip one planck short of the price bump",
        cancellation(sender, nonce + 1, bumpFee(fees.raisedFeeCap), bumpFee(fees.raisedTip).minus(1)));
    var cancel = qrl.sendTransaction(
        cancellation(sender, nonce + 1, bumpFee(fees.raisedFeeCap), bumpFee(fees.raisedTip)));
    requireHash("cancel transaction hash", cancel);
    requireReplaced("mistaken transaction", mistaken);
    requirePooled("cancel transaction", cancel);

    // Fill the nonce gap, so the cancelling transfer becomes executable.
    requireHash("gap transaction hash",
        qrl.sendTransaction(cancellation(sender, nonce, fees.raisedFeeCap, fees.raisedTip)));
    var receipt = waitForReceipt(cancel);
    if (Number(receipt.status) !== 1) {
        throw new Error("cancel transaction failed: " + JSON.stringify(receipt));
    }
    requireUnmined("mistaken transaction", mistaken);
    var balanceAfter = qrl.getBalance(recipient);
    if (balanceAfter.toString(10) !== balanceBefore.toString(10)) {
        throw new Error("mistaken transaction's recipient received " + balanceAfter.minus(balanceBefore));
    }
});

suite.finish();
