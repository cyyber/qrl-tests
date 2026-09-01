var suite = createConsoleSuite("contract");
var check = suite.check;
var deployedValue = 1337;
var eventSignature = "Deployed(uint256)";

function patternedHexData(byteLength, multiplier, addend) {
    var out = "0x";
    for (var i = 0; i < byteLength; i++) {
        var value = (i * multiplier + addend) & 0xff;
        out += (value < 16 ? "0" : "") + value.toString(16);
    }
    return out;
}

function expectTopicLengthError(topic, blockNumber, address) {
    try {
        qrl.getLogs({
            fromBlock: web3.toHex(blockNumber),
            toBlock: web3.toHex(blockNumber),
            address: address,
            topics: [topic]
        });
    } catch (error) {
        var message = String(error);
        if (message.indexOf("invalid length 32") < 0 ||
            message.indexOf("expected 64 for topic") < 0) {
            throw new Error("unexpected topic validation error: " + message);
        }
        return;
    }
    throw new Error("RPC unexpectedly accepted topic " + topic);
}

loadScript(".params.js");

var receipt = null;
check("deployment transaction is accepted and mined", function () {
    var responseHash = qrl.sendRawTransaction(PARAMS.rawTransaction);
    if (responseHash !== PARAMS.txHash) {
        throw new Error("tx hash mismatch: have " + responseHash + " want " + PARAMS.txHash);
    }
    receipt = waitForReceipt(PARAMS.txHash);
    if (Number(receipt.status) !== 1 || !receipt.contractAddress) {
        throw new Error("deployment failed: " + JSON.stringify(receipt));
    }
});

var signatureHash = web3.sha3(eventSignature);
var expectedTopic = signatureHash + zeros(64);
var nonMatchingTopic = patternedHexData(64, 0, 0xab);
var contract = qrl.contract(PARAMS.abi).at(receipt.contractAddress);

check("transaction and block APIs expose the deployment", function () {
    var tx = qrl.getTransaction(PARAMS.txHash);
    if (tx === null || tx.hash !== PARAMS.txHash || tx.from !== PARAMS.address || tx.to !== null) {
        throw new Error("unexpected transaction: " + JSON.stringify(tx));
    }
    var blockWithHashes = qrl.getBlock(receipt.blockNumber, false);
    var blockWithTransactions = qrl.getBlock(receipt.blockNumber, true);
    if (blockWithHashes.transactions.indexOf(PARAMS.txHash) < 0) {
        throw new Error("block does not include deployment hash");
    }
    for (var i = 0; i < blockWithTransactions.transactions.length; i++) {
        if (blockWithTransactions.transactions[i].hash === PARAMS.txHash) {
            return;
        }
    }
    throw new Error("block does not include deployment transaction");
});

check("receipt APIs expose one VM64 event", function () {
    if (receipt.logs.length !== 1) {
        throw new Error("expected one receipt log, got " + receipt.logs.length);
    }
    var log = receipt.logs[0];
    if (log.topics.length !== 1 || log.topics[0] !== expectedTopic) {
        throw new Error("unexpected receipt topics: " + JSON.stringify(log.topics));
    }
    var valueHex = deployedValue.toString(16);
    var expectedData = "0x" + zeros(128 - valueHex.length) + valueHex;
    if (log.data !== expectedData) {
        throw new Error("unexpected event data: " + log.data);
    }
    var receipts = qrl.getBlockReceipts(web3.toHex(receipt.blockNumber));
    for (var i = 0; i < receipts.length; i++) {
        if (receipts[i].transactionHash === PARAMS.txHash) {
            var blockLogs = receipts[i].logs;
            if (blockLogs.length !== 1 ||
                blockLogs[0].topics.length !== 1 ||
                blockLogs[0].topics[0] !== expectedTopic ||
                blockLogs[0].data !== expectedData) {
                throw new Error("unexpected block receipt");
            }
            return;
        }
    }
    throw new Error("block receipts omit deployment transaction");
});

var largeAmount = "6703903964971298549787012499102923063739682910296196688861780721860882015036773488400937149083451713845015929093243025426876941405973284973216824503046708";
var negativeDelta = "-3351951982485649274893506249551461531869841455148098344430890360930441007518386744200468574541725856922507964546621512713438470702986642486608412251520982";
var bytes64Tag = patternedHexData(64, 1, 0x80);
var multiwordPayload = patternedHexData(129, 29, 7);
var boundaryNote = "VM64 string crosses the 64-byte ABI word boundary: 0123456789abcdef0123456789abcdef";

check("contract echoes VM64 scalar and dynamic values", function () {
    var echoed = contract.echo(
        largeAmount,
        negativeDelta,
        bytes64Tag,
        PARAMS.address,
        multiwordPayload,
        boundaryNote,
        true
    );
    if (!(echoed instanceof Array) || echoed.length !== 7) {
        throw new Error("unexpected echo result: " + JSON.stringify(echoed));
    }
    if (echoed[0].toString(10) !== largeAmount || echoed[1].toString(10) !== negativeDelta) {
        throw new Error("integer mismatch");
    }
    if (echoed[2].toLowerCase() !== bytes64Tag) {
        throw new Error("fixed-width mismatch");
    }
    var expectedAddress = web3.toChecksumAddress(PARAMS.address);
    if (echoed[3] !== expectedAddress) {
        throw new Error("decoded address is not canonical: " + echoed[3]);
    }
    if (echoed[4].toLowerCase() !== multiwordPayload || echoed[5] !== boundaryNote || echoed[6] !== true) {
        throw new Error("dynamic-value mismatch");
    }
});

check("contract echoes fixed-byte boundaries", function () {
    var values = [
        "0xa5",
        patternedHexData(32, 1, 1),
        patternedHexData(33, 1, 0x40),
        bytes64Tag
    ];
    var echoed = contract.echoFixed(values[0], values[1], values[2], values[3]);
    if (!(echoed instanceof Array) || echoed.length !== values.length) {
        throw new Error("unexpected fixed-bytes result: " + JSON.stringify(echoed));
    }
    for (var i = 0; i < values.length; i++) {
        if (echoed[i].toLowerCase() !== values[i]) {
            throw new Error("fixed bytes " + i + " mismatch");
        }
    }
});

check("contract echoes fixed and dynamic arrays", function () {
    var secondTag = patternedHexData(64, 29, 7);
    var echoed = contract.echoArrays([0, 1, largeAmount], [bytes64Tag, secondTag]);
    if (!(echoed instanceof Array) || echoed.length !== 2 ||
        echoed[0].length !== 3 || echoed[1].length !== 2) {
        throw new Error("unexpected array result: " + JSON.stringify(echoed));
    }
    if (echoed[0][0].toString(10) !== "0" ||
        echoed[0][1].toString(10) !== "1" ||
        echoed[0][2].toString(10) !== largeAmount) {
        throw new Error("integer array mismatch");
    }
    if (echoed[1][0].toLowerCase() !== bytes64Tag ||
        echoed[1][1].toLowerCase() !== secondTag) {
        throw new Error("bytes64 array mismatch");
    }
});

check("contract wrapper dispatches overloaded methods", function () {
    var integer = contract.overloaded["uint512"](largeAmount);
    if (integer.toString(10) !== web3.toBigNumber(largeAmount).plus(1).toString(10)) {
        throw new Error("unexpected overloaded integer result: " + integer);
    }

    var bytes = patternedHexData(33, 7, 3);
    if (contract.overloaded["bytes33"](bytes).toLowerCase() !== bytes) {
        throw new Error("unexpected overloaded bytes result");
    }
});

check("contract wrapper propagates revert errors", function () {
    try {
        contract.failReason();
    } catch (error) {
        var message = String(error);
        if (message.indexOf("execution reverted") < 0 &&
            message.indexOf("console wrapper revert") < 0) {
            throw new Error("unexpected revert error: " + message);
        }
        return;
    }
    throw new Error("reverting contract call unexpectedly succeeded");
});

check("contract event filter decodes the emitted log", function () {
    var filter = contract.Deployed({}, {
        fromBlock: web3.toHex(receipt.blockNumber),
        toBlock: web3.toHex(receipt.blockNumber)
    });
    var events = filter.get();
    if (events.length !== 1 ||
        events[0].transactionHash !== receipt.transactionHash ||
        Number(events[0].args.value) !== deployedValue) {
        throw new Error("unexpected contract event: " + JSON.stringify(events));
    }
});

check("raw log filters support exact, wildcard, OR, and non-matching topics", function () {
    var options = {
        fromBlock: web3.toHex(receipt.blockNumber),
        toBlock: web3.toHex(receipt.blockNumber),
        address: receipt.contractAddress
    };
    options.topics = [expectedTopic];
    var exact = qrl.getLogs(options);
    options.topics = [null];
    var wildcard = qrl.getLogs(options);
    options.topics = [[nonMatchingTopic, expectedTopic]];
    var alternatives = qrl.getLogs(options);
    options.topics = [nonMatchingTopic];
    var missing = qrl.getLogs(options);
    if (exact.length !== 1 || wildcard.length !== 1 || alternatives.length !== 1) {
        throw new Error("unexpected filtered logs");
    }
    if (missing.length !== 0) {
        throw new Error("non-matching topic returned logs: " + JSON.stringify(missing));
    }
    if (exact[0].topics[0] !== expectedTopic) {
        throw new Error("unexpected exact topic: " + exact[0].topics[0]);
    }
});

check("raw log filters reject 32-byte topics", function () {
    expectTopicLengthError(signatureHash, receipt.blockNumber, receipt.contractAddress);
});

suite.finish();
