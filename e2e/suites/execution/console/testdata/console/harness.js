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
