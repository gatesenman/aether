"""Rebrand an extracted VSCodium build into 'Aether IDE' and install the Aether extension as a built-in.
usage: python3 brand.py <app-resources-dir> <platform: linux-x64|win32-x64|darwin-arm64> <aether-native-binary> <extension-src-dir>
<app-resources-dir> is the directory that contains product.json (…/resources/app)."""
import json, os, shutil, sys

app, plat, native, extsrc = sys.argv[1:5]
V = "0.5.0"
p = os.path.join(app, "product.json")
d = json.load(open(p, encoding="utf-8"))
d.update({
    "nameShort": "Aether IDE", "nameLong": "Aether IDE", "applicationName": "aether-ide",
    "dataFolderName": ".aether-ide", "sharedDataFolderName": ".aether-ide-shared", "serverDataFolderName": ".aether-ide-server",
    "serverApplicationName": "aether-ide-server", "tunnelApplicationName": "aether-ide-tunnel",
    "urlProtocol": "aether-ide", "linuxIconName": "aether-ide", "win32MutexName": "aetheride",
    "win32DirName": "Aether IDE", "win32NameVersion": "Aether IDE", "win32RegValueName": "AetherIDE",
    "win32AppUserModelId": "Aether.AetherIDE", "win32ShellNameShort": "Aether IDE",
    "win32x64AppId": "{{6A2C5B1E-AE7E-4E10-9C4B-3D2A0E4A0400}", "win32arm64AppId": "{{6A2C5B1E-AE7E-4E10-9C4B-3D2A0E4A0401}",
    "win32x64UserAppId": "{{6A2C5B1E-AE7E-4E10-9C4B-3D2A0E4A0402}", "win32arm64UserAppId": "{{6A2C5B1E-AE7E-4E10-9C4B-3D2A0E4A0403}",
    "win32TunnelServiceMutex": "aetheride-tunnelservice", "win32TunnelMutex": "aetheride-tunnel",
    "darwinBundleIdentifier": "dev.aether-lang.ide",
    "reportIssueUrl": "https://example.invalid/aether-lang/aether/issues",
    "aetherIdeVersion": V,
    # Open VSX marketplace (inherited from VSCodium, set explicitly)
    "extensionsGallery": {"serviceUrl": "https://open-vsx.org/vscode/gallery", "itemUrl": "https://open-vsx.org/vscode/item",
                          "latestUrlTemplate": "https://open-vsx.org/vscode/gallery/{publisher}/{name}/latest",
                          "controlUrl": "https://raw.githubusercontent.com/EclipseFdn/publish-extensions/refs/heads/master/extension-control/extensions.json"},
})
d.pop("serverDownloadUrlTemplate", None)  # no Aether-branded remote server build is published
json.dump(d, open(p, "w", encoding="utf-8"), indent="\t", ensure_ascii=False)

# built-in Aether extension (with the native binary for this platform)
dst = os.path.join(app, "extensions", "aether-lang.aether-vscode")
shutil.rmtree(dst, ignore_errors=True)
shutil.copytree(extsrc, dst, ignore=shutil.ignore_patterns("node_modules", "src", "test", "*.vsix", "package.sh", "package-lock.json", "bin"))
node_plat = {"linux-x64": "linux-x64", "win32-x64": "win32-x64", "darwin-arm64": "darwin-arm64", "darwin-x64": "darwin-x64"}[plat]
exe = "aether.exe" if plat.startswith("win32") else "aether"
os.makedirs(os.path.join(dst, "bin", node_plat), exist_ok=True)
shutil.copy2(native, os.path.join(dst, "bin", node_plat, exe))
os.chmod(os.path.join(dst, "bin", node_plat, exe), 0o755)
pj = os.path.join(dst, "package.json")
pk = json.load(open(pj, encoding="utf-8"))
pk.pop("devDependencies", None); pk.pop("dependencies", None); pk.pop("scripts", None)
# IDE layout: agent panel lives in the right-hand secondary side bar
agent = [v for v in pk["contributes"]["views"]["aether"] if v["id"] == "aether.agent"]
pk["contributes"]["views"]["aether"] = [v for v in pk["contributes"]["views"]["aether"] if v["id"] != "aether.agent"]
pk["contributes"]["viewsContainers"]["secondarySidebar"] = [{"id": "aether-agent", "title": "Aether 智能体", "icon": "media/activity.svg"}]
pk["contributes"]["views"]["aether-agent"] = agent
# IDE default settings (theme, layout, no third-party AI chat UI)
pk["contributes"]["configurationDefaults"].update({
    "workbench.colorTheme": "Aether Dark",
    "workbench.secondarySideBar.defaultVisibility": "visible",
    "chat.disableAIFeatures": True,
    "window.commandCenter": True,
    "workbench.layoutControl.enabled": True,
    "workbench.startupEditor": "welcomePage",
    "editor.minimap.enabled": True,
    "editor.fontSize": 14,
    "files.associations": {"*.ae": "aether"},
    "telemetry.telemetryLevel": "off",
})
json.dump(pk, open(pj, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
print("branded", app, plat)
