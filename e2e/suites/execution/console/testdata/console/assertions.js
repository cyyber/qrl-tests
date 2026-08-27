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
