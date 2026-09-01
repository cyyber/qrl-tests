var suite = createConsoleSuite("topics");
var check = suite.check;

loadScript(".params.js");

function sameTopics(got, want) {
    if (got.length !== want.length) {
        return false;
    }
    for (var i = 0; i < got.length; i++) {
        if (got[i].toLowerCase() !== want[i].toLowerCase()) {
            return false;
        }
    }
    return true;
}

var receipt = null;
check("indexed event transaction is accepted and mined", function () {
    var txHash = qrl.sendRawTransaction(PARAMS.indexedRawTransaction);
    if (txHash !== PARAMS.indexedTxHash) {
        throw new Error("tx hash mismatch: have " + txHash + " want " + PARAMS.indexedTxHash);
    }
    receipt = waitForReceipt(txHash);
    if (Number(receipt.status) !== 1 || !receipt.contractAddress) {
        throw new Error("indexed event transaction failed: " + JSON.stringify(receipt));
    }
});

check("receipt preserves indexed VM64 topics", function () {
    if (receipt.logs.length !== 3 ||
        !sameTopics(receipt.logs[0].topics, PARAMS.numberTopics) ||
        !sameTopics(receipt.logs[1].topics, PARAMS.bytesTopics) ||
        !sameTopics(receipt.logs[2].topics, PARAMS.referenceTopics)) {
        throw new Error("unexpected indexed topics: " + JSON.stringify(receipt.logs));
    }
});

var contract = qrl.contract(PARAMS.indexedABI).at(receipt.contractAddress);
var block = web3.toHex(receipt.blockNumber);

check("generated filters encode and decode indexed bool and 512-bit integers", function () {
    var events = contract.IndexedNumbers({
        flag: true,
        delta: PARAMS.indexedDelta,
        amount: PARAMS.indexedAmount
    }, {fromBlock: block, toBlock: block}).get();
    if (events.length !== 1 ||
        events[0].args.flag !== true ||
        events[0].args.delta.toString(10) !== PARAMS.indexedDelta ||
        events[0].args.amount.toString(10) !== PARAMS.indexedAmount) {
        throw new Error("unexpected indexed scalar event: " + JSON.stringify(events));
    }
});

check("generated filters preserve indexed bytes33 alignment", function () {
    var events = contract.IndexedBytes({
        code: PARAMS.indexedCode
    }, {fromBlock: block, toBlock: block}).get();
    if (events.length !== 1 ||
        events[0].args.code.toLowerCase() !== PARAMS.indexedCode.toLowerCase()) {
        throw new Error("unexpected indexed bytes33 event: " + JSON.stringify(events));
    }
});

check("generated filters encode and decode indexed addresses and dynamic values", function () {
    var expectedAddress = web3.toChecksumAddress(PARAMS.address);
    var events = contract.IndexedReference({
        account: PARAMS.address,
        label: PARAMS.indexedLabel,
        payload: PARAMS.indexedPayload
    }, {fromBlock: block, toBlock: block}).get();
    if (events.length !== 1 ||
        events[0].args.account !== expectedAddress ||
        !web3.isChecksumAddress(events[0].args.account) ||
        events[0].args.label.toLowerCase() !== PARAMS.indexedLabelTopic.toLowerCase() ||
        events[0].args.payload.toLowerCase() !== PARAMS.indexedPayloadTopic.toLowerCase()) {
        throw new Error("unexpected indexed reference event: " + JSON.stringify(events));
    }
});

suite.finish();
