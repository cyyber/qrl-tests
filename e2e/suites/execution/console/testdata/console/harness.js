function createConsoleSuite(name) {
    return {
        check: function (description, assertion) {
            try {
                assertion();
            } catch (error) {
                console.error("CONSOLE_E2E_FAIL " + name + " " + description + " -- " + error);
                throw error;
            }
            console.log("PASS: " + description);
        },
        finish: function () {
            console.log("CONSOLE_E2E_PASS " + name);
        }
    };
}

function waitForReceipt(transactionHash) {
    for (var attempt = 0; attempt < 60; attempt++) {
        var receipt = qrl.getTransactionReceipt(transactionHash);
        if (receipt !== null && receipt.blockNumber !== null) {
            return receipt;
        }
        admin.sleep(5);
    }
    throw new Error("transaction not mined within timeout: " + transactionHash);
}

function zeros(length) {
    return new Array(length + 1).join("0");
}
