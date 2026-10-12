var suite = createConsoleSuite("replacement");
var check = suite.check;

// The pool's default --txpool.pricebump: a replacement transaction must raise
// both fees by this percentage.
var priceBump = 10;

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
var mistakenRecipient = web3.toChecksumAddress("Q" + new Array(65).join("a7"));
requireAddress("recipient", recipient);
requireAddress("mistaken transaction recipient", mistakenRecipient);

function bumpFee(fee) {
    var bumped = fee.times(100 + priceBump).dividedToIntegerBy(100);
    return bumped.gt(fee) ? bumped : fee.plus(1);
}

function transfer(to, value, nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return {
        from: sender,
        to: to,
        value: value,
        nonce: nonce,
        gas: transferGas,
        maxFeePerGas: maxFeePerGas,
        maxPriorityFeePerGas: maxPriorityFeePerGas
    };
}

// payment sends one planck from the sender.
function payment(to, nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return transfer(to, 1, nonce, maxFeePerGas, maxPriorityFeePerGas);
}

// cancellation sends nothing from the sender to itself: it takes a nonce and
// does nothing else.
function cancellation(nonce, maxFeePerGas, maxPriorityFeePerGas) {
    return transfer(sender, 0, nonce, maxFeePerGas, maxPriorityFeePerGas);
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

function requireInPool(description, transactionHash) {
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

function requireNotMined(description, transactionHash) {
    if (qrl.getTransactionReceipt(transactionHash) !== null) {
        throw new Error(description + " was mined");
    }
}

function requireMined(description, transactionHash) {
    var receipt = waitForReceipt(transactionHash);
    if (Number(receipt.status) !== 1) {
        throw new Error(description + " failed: " + JSON.stringify(receipt));
    }
}

function requireReceived(description, recipient, balanceBefore, value) {
    var received = qrl.getBalance(recipient).minus(balanceBefore);
    if (received.toString(10) !== String(value)) {
        throw new Error(description + " received " + received + ", want " + value);
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
        requireInPool("stuck transaction", stuck);
        // The pending nonce moved: the transaction has the sender's next
        // nonce, and only its fees keep it out of a block.
        if (qrl.getTransactionCount(sender, "pending") !== nonce + 1) {
            throw new Error("stuck transaction is not executable");
        }

        requireUnderpriced("replacement transaction with the same fees",
            cancellation(nonce, stuckFeeCap, stuckTip));
        requireUnderpriced("replacement transaction with only the fee cap raised",
            cancellation(nonce, bumpFee(stuckFeeCap), stuckTip));
        requireUnderpriced("replacement transaction with only the tip raised",
            cancellation(nonce, stuckFeeCap, bumpFee(stuckTip)));
        requireInPool("stuck transaction", stuck);
    } catch (error) {
        failure = error;
    }

    // The stuck transaction's payment again, with fees that cover the base
    // fee.
    var raised = sendDespite(failure, payment(recipient, nonce, feeCap, tip));
    requireMined("raised transaction", raised);
    requireNotMined("stuck transaction", stuck);
    requireReceived("recipient", recipient, balanceBefore, 1);
});

// A transaction sent by mistake one nonce ahead waits in the pool's queue,
// where no block can include it, until the gap is filled. Its sender cancels
// it there with a transfer of nothing to itself, which is mined once the gap
// is filled.
check("node-managed signer cancels a transaction sent one nonce ahead", function () {
    var nonce = qrl.getTransactionCount(sender, "pending");
    var balanceBefore = qrl.getBalance(mistakenRecipient);

    var mistaken = qrl.sendTransaction(payment(mistakenRecipient, nonce + 1, feeCap, tip));
    requireInPool("mistaken transaction", mistaken);
    if (qrl.getTransactionCount(sender, "pending") !== nonce) {
        throw new Error("mistaken transaction is not queued behind the nonce gap");
    }

    // With normal fees the price bump is far more than a planck, so the "one
    // planck short" cases pin it exactly.
    requireUnderpriced("cancelling transfer with the same fees",
        cancellation(nonce + 1, feeCap, tip));
    requireUnderpriced("cancelling transfer with the fee cap one planck short of the price bump",
        cancellation(nonce + 1, bumpFee(feeCap).minus(1), bumpFee(tip)));
    requireUnderpriced("cancelling transfer with the tip one planck short of the price bump",
        cancellation(nonce + 1, bumpFee(feeCap), bumpFee(tip).minus(1)));
    var cancel = qrl.sendTransaction(cancellation(nonce + 1, bumpFee(feeCap), bumpFee(tip)));
    requireReplaced("mistaken transaction", mistaken);
    requireInPool("cancelling transfer", cancel);

    // Fill the nonce gap, so the cancelling transfer becomes executable.
    qrl.sendTransaction(cancellation(nonce, feeCap, tip));
    requireMined("cancelling transfer", cancel);
    requireNotMined("mistaken transaction", mistaken);
    requireReceived("mistaken transaction's recipient", mistakenRecipient, balanceBefore, 0);
});

suite.finish();
