var suite = createConsoleSuite("api");
var check = suite.check;

function requireHexQuantity(name, value) {
    if (typeof value !== "string" || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/.test(value)) {
        throw new Error(name + " is not a hex quantity: " + value);
    }
}

function requireHash(name, value) {
    if (typeof value !== "string" || !/^0x[0-9a-f]{64}$/i.test(value)) {
        throw new Error(name + " is not a 32-byte hash: " + value);
    }
}

function requireAddress(name, value) {
    if (typeof value !== "string" || !/^Q[0-9a-fA-F]{128}$/.test(value)) {
        throw new Error(name + " is not a QRL address: " + value);
    }
}

check("block APIs agree", function () {
    var blockNumber = qrl.blockNumber;
    if (typeof blockNumber !== "number" || blockNumber <= 0) {
        throw new Error("unexpected qrl.blockNumber: " + blockNumber);
    }
    var block = qrl.getBlock(blockNumber);
    requireHash("block hash", block.hash);
    requireAddress("block fee recipient", block.miner);
    var byHash = qrl.getBlock(block.hash);
    if (!byHash || byHash.hash !== block.hash || byHash.number !== block.number) {
        throw new Error("block lookup mismatch");
    }
});

check("provider dispatch and console namespaces respond", function () {
    var response = web3.currentProvider.send({
        jsonrpc: "2.0",
        id: 1,
        method: "rpc_modules",
        params: []
    });
    if (response.error) {
        throw new Error("rpc_modules: " + JSON.stringify(response.error));
    }
    var modules = response.result;
    ["admin", "net", "qrl", "txpool", "web3"].forEach(function (name) {
        if (typeof modules[name] !== "string") {
            throw new Error("missing rpc module " + name);
        }
    });
    if (typeof web3.version.node !== "string") {
        throw new Error("web3.version.node did not return a string");
    }
    if (typeof net.version !== "string" || typeof net.listening !== "boolean" ||
        typeof net.peerCount !== "number") {
        throw new Error("unexpected net namespace");
    }
    if (!admin.nodeInfo || !(admin.peers instanceof Array)) {
        throw new Error("unexpected admin namespace");
    }
    var status = txpool.status;
    if (typeof status.pending !== "number" || typeof status.queued !== "number") {
        throw new Error("unexpected txpool namespace");
    }
});

check("qrl.chainId matches the network ID", function () {
    var chainID = qrl.chainId();
    requireHexQuantity("qrl.chainId", chainID);
    var chainIDDecimal = web3.toBigNumber(chainID).toString(10);
    var networkIDDecimal = web3.toBigNumber(net.version).toString(10);
    if (chainIDDecimal !== networkIDDecimal) {
        throw new Error("chain ID " + chainIDDecimal + " does not match network ID " + networkIDDecimal);
    }
});

check("header API returns the latest header", function () {
    var header = qrl.getHeaderByNumber("latest");
    requireHash("header hash", header.hash);
    requireHash("header parentHash", header.parentHash);
    requireAddress("header fee recipient", header.miner);
});

check("state and fee APIs respond", function () {
    var miner = qrl.getBlock("latest").miner;
    var balance = qrl.getBalance(miner, "latest");
    if (!balance.gte(0)) {
        throw new Error("invalid balance: " + balance);
    }
    var nonce = qrl.getTransactionCount(miner, "latest");
    if (typeof nonce !== "number" || nonce < 0) {
        throw new Error("invalid nonce: " + nonce);
    }
    var gasPrice = qrl.gasPrice;
    var priorityFee = qrl.maxPriorityFeePerGas;
    if (!gasPrice.gt(0) || !priorityFee.gte(0)) {
        throw new Error(
            "invalid fee data: gasPrice=" + gasPrice +
            ", maxPriorityFeePerGas=" + priorityFee
        );
    }
});

check("qrl.feeHistory returns coherent history", function () {
    var history = qrl.feeHistory(1, "latest", []);
    requireHexQuantity("oldestBlock", history.oldestBlock);
    if (!(history.baseFeePerGas instanceof Array) || history.baseFeePerGas.length !== 2) {
        throw new Error("unexpected baseFeePerGas: " + JSON.stringify(history));
    }
    history.baseFeePerGas.forEach(function (baseFee, index) {
        requireHexQuantity("baseFeePerGas[" + index + "]", baseFee);
    });
    if (!(history.gasUsedRatio instanceof Array) || history.gasUsedRatio.length !== 1) {
        throw new Error("unexpected gasUsedRatio: " + JSON.stringify(history));
    }
    var gasUsedRatio = history.gasUsedRatio[0];
    if (typeof gasUsedRatio !== "number" || !(gasUsedRatio >= 0 && gasUsedRatio <= 1)) {
        throw new Error("invalid gasUsedRatio: " + gasUsedRatio);
    }
});

check("QIP-55 Q-address checksum round-trips", function () {
    var miner = qrl.getBlock(qrl.blockNumber).miner;
    requireAddress("miner", miner);
    var lower = "Q" + miner.slice(1).toLowerCase();
    var checksummed = web3.toChecksumAddress(lower);
    if (!web3.isChecksumAddress(checksummed) || !web3.isAddress(checksummed)) {
        throw new Error("invalid checksummed address: " + checksummed);
    }
    if ("Q" + checksummed.slice(1).toLowerCase() !== lower) {
        throw new Error("checksumming changed the address bytes");
    }
    for (var i = 1; i < checksummed.length; i++) {
        var character = checksummed.charAt(i);
        if (/[a-fA-F]/.test(character)) {
            var flipped = character === character.toLowerCase() ?
                character.toUpperCase() : character.toLowerCase();
            var mangled = checksummed.slice(0, i) + flipped + checksummed.slice(i + 1);
            if (web3.isChecksumAddress(mangled)) {
                throw new Error("case-mangled address passes checksum validation");
            }
            break;
        }
    }
});

suite.finish();
