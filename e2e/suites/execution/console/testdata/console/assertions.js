function requireHexQuantity(name, value) {
    if (typeof value !== "string" || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/.test(value)) {
        throw new Error(name + " is not a hex quantity: " + value);
    }
}

function requireDecimalString(name, value) {
    if (typeof value !== "string" || !/^(?:0|[1-9][0-9]*)$/.test(value)) {
        throw new Error(name + " is not a decimal string: " + value);
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
    if (!web3.isChecksumAddress(value)) {
        throw new Error(name + " is not a checksummed QRL address: " + value);
    }
}

function requireNonNegativeInteger(name, value) {
    if (typeof value !== "number" || value < 0 || value % 1 !== 0) {
        throw new Error(name + " is not a nonnegative integer: " + value);
    }
}
