function createConsoleSuite(name) {
    return {
        check: function (desc, fn) {
            try {
                if (fn() === false) {
                    throw new Error("assertion returned false");
                }
            } catch (error) {
                console.error("CONSOLE_E2E_FAIL " + name + " " + desc + " -- " + error);
                throw error;
            }
            console.log("PASS: " + desc);
        },
        finish: function () {
            console.log("CONSOLE_E2E_PASS " + name);
        }
    };
}
