var suite = createConsoleSuite("api");
var check = suite.check;

check("block APIs agree", function () {
    var blockNumber = qrl.blockNumber;
    requireNonNegativeInteger("qrl.blockNumber", blockNumber);
    if (blockNumber === 0) {
        throw new Error("unexpected qrl.blockNumber: " + blockNumber);
    }
    var block = qrl.getBlock(blockNumber);
    requireHash("block hash", block.hash);
    requireAddress("block fee recipient", block.miner);
    if (block.number !== blockNumber) {
        throw new Error("block number mismatch: got " + block.number + ", want " + blockNumber);
    }
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
    ["admin", "debug", "net", "qrl", "rpc", "txpool", "web3"].forEach(function (name) {
        if (modules[name] !== "1.0") {
            throw new Error("unexpected rpc module " + name + ": " + modules[name]);
        }
    });
    if (typeof modules.engine !== "undefined") {
        throw new Error("authenticated engine module is exposed by public RPC");
    }
    var clientVersion = web3.version.node;
    var listening = net.listening;
    var peerCount = net.peerCount;
    var nodeInfo = admin.nodeInfo;
    var peers = admin.peers;
    if (typeof clientVersion !== "string" || clientVersion.indexOf("Gqrl/") !== 0) {
        throw new Error("web3.version.node did not return a client version");
    }
    requireNonNegativeInteger("net.peerCount", peerCount);
    if (listening !== true) {
        throw new Error("unexpected net namespace");
    }
    if (!nodeInfo || nodeInfo.name !== clientVersion || !(peers instanceof Array)) {
        throw new Error("unexpected admin namespace");
    }
    var status = txpool.status;
    requireNonNegativeInteger("txpool.status.pending", status.pending);
    requireNonNegativeInteger("txpool.status.queued", status.queued);
});

check("chain and network IDs are canonical", function () {
    var chainID = qrl.chainId();
    var networkID = net.version;
    requireHexQuantity("qrl.chainId", chainID);
    requireDecimalString("net.version", networkID);
});

check("header APIs agree with block data", function () {
    var blockNumber = qrl.blockNumber;
    requireNonNegativeInteger("qrl.blockNumber", blockNumber);
    var block = qrl.getBlock(blockNumber);
    var header = qrl.getHeaderByNumber(blockNumber);
    requireHash("header hash", header.hash);
    requireHash("header parentHash", header.parentHash);
    requireAddress("header fee recipient", header.miner);
    requireHexQuantity("header number", header.number);
    if (header.hash !== block.hash || header.parentHash !== block.parentHash ||
        header.miner !== block.miner || web3.toDecimal(header.number) !== block.number) {
        throw new Error("header does not match block data");
    }
    var byHash = qrl.getHeaderByHash(header.hash);
    if (!byHash || byHash.hash !== header.hash || byHash.number !== header.number ||
        byHash.parentHash !== header.parentHash || byHash.miner !== header.miner) {
        throw new Error("header lookup mismatch");
    }
});

check("state and fee APIs respond", function () {
    var miner = qrl.getBlock("latest").miner;
    var balance = qrl.getBalance(miner, "latest");
    if (!balance.gte(0)) {
        throw new Error("invalid balance: " + balance);
    }
    var nonce = qrl.getTransactionCount(miner, "latest");
    requireNonNegativeInteger("nonce", nonce);
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
    var lower = "Q" + qrl.getBlock("latest").miner.slice(1).toLowerCase();
    var checksummed = web3.toChecksumAddress(lower);
    if (!web3.isChecksumAddress(checksummed) || !web3.isAddress(checksummed)) {
        throw new Error("invalid checksummed address: " + checksummed);
    }
    if ("Q" + checksummed.slice(1).toLowerCase() !== lower) {
        throw new Error("checksumming changed the address bytes");
    }
    var mangled = checksummed.replace(/[a-fA-F]/, function (character) {
        return character === character.toLowerCase() ?
            character.toUpperCase() : character.toLowerCase();
    });
    if (mangled === checksummed || web3.isChecksumAddress(mangled)) {
        throw new Error("checksum mutation was not rejected: " + mangled);
    }
});

suite.finish();
