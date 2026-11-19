// Beginner scenario, part 1: first start → walkthrough → "new project" wizard → project created (window reloads into it)
const { vscode, sleep, waitFor, shot, runner, fs, path } = require("./common");
exports.run = async function () {
  const { step } = runner(process.env.AETHER_E2E_OUT);
  const target = process.env.AETHER_E2E_PROJECT;
  await step("extension activates", async () => {
    const ext = vscode.extensions.getExtension("aether-lang.aether-vscode"); await ext.activate();
  });
  await step("onboarding walkthrough (中文)", async () => {
    await vscode.commands.executeCommand("workbench.action.openWalkthrough", "aether-lang.aether-vscode#aether.gettingStarted", false);
    await sleep(2500); if (!shot("v05_e2e_01_walkthrough")) throw new Error("screenshot failed");
  });
  await step("new project wizard (template picker)", async () => {
    vscode.commands.executeCommand("aether.newProject"); // opens the quick pick exactly as the button does
    await sleep(1500); shot("v05_e2e_02_new_project_wizard");
    await vscode.commands.executeCommand("workbench.action.closeQuickOpen"); await sleep(300);
  });
  await step("create project from template crm", async () => {
    let err = null;
    vscode.commands.executeCommand("aether.newProject", "crm", target).then(null, (e) => (err = e)); // → `aether new`, then opens the folder (window reload)
    try { await waitFor(() => err || (fs.existsSync(path.join(target, "main.ae")) && fs.existsSync(path.join(target, "README.md")))); }
    catch (e) { throw new Error("project not created; PATH=" + process.env.PATH); }
    if (err) throw err;
  });
  await sleep(8000); // window reloads into the project; phase 2 continues there
};
