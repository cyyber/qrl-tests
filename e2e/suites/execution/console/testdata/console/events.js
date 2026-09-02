var suite = createConsoleSuite("events");
var check = suite.check;

loadScript(".params.js");

var deploymentReceipt = null;
check("raw deployment transaction is accepted and mined", function () {
    var deploymentHash = qrl.sendRawTransaction(PARAMS.rawTransaction);
    if (deploymentHash !== PARAMS.txHash) {
        throw new Error("deployment transaction hash mismatch: expected " +
            PARAMS.txHash + ", got " + deploymentHash);
    }
    deploymentReceipt = waitForReceipt(deploymentHash);
    if (Number(deploymentReceipt.status) !== 1 || !deploymentReceipt.contractAddress) {
        throw new Error("deployment transaction failed: " + JSON.stringify(deploymentReceipt));
    }
});

var contract = qrl.contract(PARAMS.abi).at(deploymentReceipt.contractAddress);
var expectedLabelTopic = web3.sha3(PARAMS.storeLabel) + zeros(64);
var expectedPayloadTopic = web3.sha3(PARAMS.storePayload, {encoding: "hex"}) + zeros(64);

var managed = qrl.accounts;
if (!(managed instanceof Array) || managed.length === 0) {
    throw new Error("unexpected node-managed accounts: " + JSON.stringify(managed));
}
var sender = web3.toChecksumAddress(managed[0]);
requireAddress("node-managed account", sender);

check("state-changing wrapper builds the expected request", function () {
    var request = contract.store.request(
        PARAMS.storeValue,
        PARAMS.storeLabel,
        PARAMS.storePayload,
        {from: sender, gas: 500000}
    );
    if (request.method !== "qrl_sendTransaction" ||
        request.params.length !== 1 ||
        request.params[0].data !== PARAMS.storeData) {
        throw new Error("unexpected state-changing wrapper request");
    }
});

var storeReceiptPolls = 0;
var storeReceiptPollLimit = 60;
var storeReceiptPollInterval = 5000;
var storeReceiptTimer = null;
var storeTransactionHash = null;

function stopStoreReceiptMonitor() {
    if (storeReceiptTimer !== null) {
        clearInterval(storeReceiptTimer);
        storeReceiptTimer = null;
    }
}

function failEvents(failure) {
    stopStoreReceiptMonitor();
    console.error("CONSOLE_E2E_FAIL events " + failure);
    if (watcher && watcher.filterId !== null) {
        watcher.stopWatching();
    }
}

var watcher = contract.Stored({
    sender: sender,
    label: PARAMS.storeLabel,
    payload: PARAMS.storePayload
}, {fromBlock: "latest"});
if (watcher.filterId === null) {
    var filterFailure = new Error("Stored event filter creation failed");
    failEvents(filterFailure);
    throw filterFailure;
}
watcher.watch(function (error, event) {
    try {
        if (error) {
            throw error;
        }
        var receipt = qrl.getTransactionReceipt(storeTransactionHash);
        check("state-changing contract wrapper call is mined", function () {
            if (receipt === null || receipt.blockNumber === null || Number(receipt.status) !== 1 ||
                receipt.transactionHash !== storeTransactionHash) {
                throw new Error("store transaction failed: " + JSON.stringify(receipt));
            }
            var transaction = qrl.getTransaction(storeTransactionHash);
            if (transaction === null ||
                web3.toChecksumAddress(transaction.from) !== sender ||
                web3.toChecksumAddress(transaction.to) !== deploymentReceipt.contractAddress) {
                throw new Error("unexpected store transaction: " + JSON.stringify(transaction));
            }
        });

        check("state wrappers return the full VM64 storage value", function () {
            var expected = "0x" + web3.toBigNumber(PARAMS.storeValue).toString(16);
            if (expected.length !== 130) {
                throw new Error("fixture is not a full-width VM64 value: " + expected);
            }
            var stored = qrl.getStorageAt(deploymentReceipt.contractAddress, "0x0", "latest");
            if (stored.toLowerCase() !== expected) {
                throw new Error("unexpected storage value: " + stored);
            }
            var proof = qrl.getProof(deploymentReceipt.contractAddress, ["0x0"], "latest");
            if (!proof.storageProof || proof.storageProof.length !== 1 ||
                proof.storageProof[0].value.toLowerCase() !== expected) {
                throw new Error("unexpected storage proof: " + JSON.stringify(proof));
            }
        });

        check("WebSocket event watch decodes indexed dynamic fields", function () {
            if (event.transactionHash !== storeTransactionHash) {
                throw new Error("event watch returned the wrong transaction");
            }
            if (event.args.sender !== sender ||
                !web3.isChecksumAddress(event.args.sender)) {
                throw new Error("event sender is not canonical: " + event.args.sender);
            }
            if (event.args.label !== expectedLabelTopic ||
                event.args.payload !== expectedPayloadTopic) {
                throw new Error("indexed dynamic topic mismatch: " + JSON.stringify(event.args));
            }
            if (event.args.value.toString(10) !== PARAMS.storeValue) {
                throw new Error("event value mismatch");
            }
        });

        check("payable wrapper forwards value", function () {
            var marker = 17;
            var payment = 23;
            var txHash = contract.pay(marker, {
                from: sender,
                value: payment,
                gas: 500000
            });
            var paidReceipt = waitForReceipt(txHash);
            if (Number(paidReceipt.status) !== 1) {
                throw new Error("payable transaction failed: " + JSON.stringify(paidReceipt));
            }
            var transaction = qrl.getTransaction(txHash);
            if (transaction.value.toString(10) !== String(payment) ||
                contract.stored().toString(10) !== String(marker + payment)) {
                throw new Error("payable wrapper did not forward value");
            }
        });

        check("state-changing wrapper exposes a failed receipt", function () {
            var stored = contract.stored().toString(10);
            var txHash = contract.failTransaction({from: sender, gas: 500000});
            var failedReceipt = waitForReceipt(txHash);
            if (Number(failedReceipt.status) !== 0) {
                throw new Error("reverting transaction unexpectedly succeeded: " +
                    JSON.stringify(failedReceipt));
            }
            if (contract.stored().toString(10) !== stored) {
                throw new Error("reverting transaction changed contract state");
            }
        });
        stopStoreReceiptMonitor();
        suite.finish();
        watcher.stopWatching();
    } catch (failure) {
        failEvents(failure);
    }
});

try {
    storeTransactionHash = contract.store(
        PARAMS.storeValue,
        PARAMS.storeLabel,
        PARAMS.storePayload,
        {from: sender, gas: 500000}
    );
    requireHash("store transaction hash", storeTransactionHash);
    console.log("PASS: state-changing wrapper submits through the node-managed signer");
} catch (failure) {
    failEvents(failure);
    throw failure;
}
storeReceiptTimer = setInterval(function () {
    if (storeReceiptTimer === null) {
        return;
    }
    try {
        storeReceiptPolls++;
        var receipt = qrl.getTransactionReceipt(storeTransactionHash);
        if (receipt !== null && receipt.blockNumber !== null &&
            Number(receipt.status) !== 1) {
            throw new Error("store transaction failed: " + JSON.stringify(receipt));
        }
        if (storeReceiptPolls >= storeReceiptPollLimit) {
            if (receipt === null || receipt.blockNumber === null) {
                throw new Error("store transaction not mined within timeout: " +
                    storeTransactionHash);
            }
            throw new Error("matching Stored event not observed within timeout: " +
                storeTransactionHash);
        }
    } catch (failure) {
        failEvents(failure);
    }
}, storeReceiptPollInterval);
