var suite = createConsoleSuite("constructor");
var check = suite.check;

loadScript(".params.js");

var managed = qrl.accounts;
if (!(managed instanceof Array) || managed.length === 0) {
    throw new Error("unexpected node-managed accounts: " + JSON.stringify(managed));
}

var contractABI = PARAMS.abi.filter(function (entry) {
    return entry.type !== "constructor";
}).concat(PARAMS.constructorABI);
var callbackCount = 0;
var receiptPolls = 0;
var receiptPollLimit = 60;
var receiptPollInterval = 5000;
var receiptTimer = null;

function stopReceiptMonitor() {
    if (receiptTimer !== null) {
        clearInterval(receiptTimer);
        receiptTimer = null;
    }
}

function failConstructor(failure) {
    console.error("CONSOLE_E2E_FAIL constructor " + failure);
    stopReceiptMonitor();
}

qrl.contract(contractABI).new(
    PARAMS.storeValue,
    PARAMS.recipient,
    PARAMS.constructorTag,
    PARAMS.constructorPayload,
    {from: managed[0], data: PARAMS.bytecode, gas: PARAMS.constructorGas},
    function (error, contract) {
        try {
            if (error) {
                throw error;
            }
            callbackCount++;
            if (callbackCount === 1) {
                if (!contract.transactionHash || contract.address) {
                    throw new Error("unexpected initial constructor callback");
                }
                receiptTimer = setInterval(function () {
                    try {
                        receiptPolls++;
                        var receipt = qrl.getTransactionReceipt(contract.transactionHash);
                        if (receipt !== null && receipt.blockNumber !== null &&
                            Number(receipt.status) !== 1) {
                            throw new Error("constructor transaction failed: " +
                                JSON.stringify(receipt));
                        }
                        if (receiptPolls >= receiptPollLimit) {
                            if (receipt === null || receipt.blockNumber === null) {
                                throw new Error("constructor transaction not mined within timeout: " +
                                    contract.transactionHash);
                            }
                            throw new Error("constructor completion callback not observed within timeout: " +
                                contract.transactionHash);
                        }
                    } catch (failure) {
                        failConstructor(failure);
                    }
                }, receiptPollInterval);
                return;
            }
            if (callbackCount !== 2) {
                throw new Error("unexpected constructor callback count: " + callbackCount);
            }

            stopReceiptMonitor();
            check("ContractFactory.new encodes and mines VM64 constructor data", function () {
                var receipt = qrl.getTransactionReceipt(contract.transactionHash);
                var transaction = qrl.getTransaction(contract.transactionHash);
                if (receipt === null || Number(receipt.status) !== 1 ||
                    receipt.contractAddress !== contract.address ||
                    transaction.input.toLowerCase() !== PARAMS.constructorInput.toLowerCase()) {
                    throw new Error("unexpected constructor deployment: " +
                        JSON.stringify({receipt: receipt, transaction: transaction}));
                }
                if (!web3.isChecksumAddress(contract.address) || qrl.getCode(contract.address) === "0x") {
                    throw new Error("constructor returned an invalid deployed contract");
                }
                if (contract.stored().toString(10) !== "0") {
                    throw new Error("deployed contract methods were not attached");
                }
            });
            suite.finish();
        } catch (failure) {
            failConstructor(failure);
        }
    }
);
