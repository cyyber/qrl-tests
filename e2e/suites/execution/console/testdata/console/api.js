var suite = createConsoleSuite("api");
var check = suite.check;

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
    var clientVersion = web3.version.node;
    var listening = net.listening;
    var peerCount = net.peerCount;
    var nodeInfo = admin.nodeInfo;
    var peers = admin.peers;
    if (typeof clientVersion !== "string" || clientVersion === "") {
        throw new Error("web3.version.node did not return a client version");
    }
    if (listening !== true || typeof peerCount !== "number" ||
        peerCount < 0 || peerCount % 1 !== 0) {
        throw new Error("unexpected net namespace");
    }
    if (!nodeInfo || nodeInfo.name !== clientVersion || !(peers instanceof Array)) {
        throw new Error("unexpected admin namespace");
    }
    if (EXPECTED !== null) {
        var expectedPeerCount = EXPECTED.execution_peer_count;
        if (peerCount !== expectedPeerCount || peers.length !== expectedPeerCount) {
            throw new Error(
                "unexpected execution peer count: net=" + peerCount +
                ", admin=" + peers.length +
                ", want=" + expectedPeerCount
            );
        }
    }
    var status = txpool.status;
    if (typeof status.pending !== "number" || typeof status.queued !== "number") {
        throw new Error("unexpected txpool namespace");
    }
});

check("chain and network IDs are canonical", function () {
    var chainID = qrl.chainId();
    var networkID = net.version;
    requireHexQuantity("qrl.chainId", chainID);
    requireDecimalString("net.version", networkID);
    if (EXPECTED !== null) {
        if (chainID !== EXPECTED.chain_id) {
            throw new Error("unexpected chain ID: got " + chainID + ", want " + EXPECTED.chain_id);
        }
        if (networkID !== EXPECTED.network_id) {
            throw new Error("unexpected network ID: got " + networkID + ", want " + EXPECTED.network_id);
        }
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
