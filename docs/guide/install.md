# Install, upgrade, and uninstall

This page gives the commands for installing, upgrading, and uninstalling OffiCraft on macOS, Linux, and Windows (through WSL2), and explains what each action means and does when you add a second computer to the same studio or remove a machine from the studio.

Every command on this page runs in a terminal: the built-in Terminal app on macOS, the system terminal on Linux, and the Ubuntu window inside WSL2 on Windows (see "Windows: preparing WSL2"). The sections starting with "Advanced" cover non-default install methods and options.

---

## Installing

### System requirements

- macOS (Apple Silicon), Linux (x86_64, arm64), or Windows 10 (version 2004 or later) / 11 (through WSL2)
- tmux 3.0 or later (a terminal tool that keeps programs running in the background). If it is missing, the install command installs it automatically: with Homebrew on macOS that has [Homebrew](https://brew.sh), and with `apt-get` (the package installer on Ubuntu and similar Linux distributions) on Linux and WSL2 that have `apt-get`; before installing, it warns that you may need to enter your Linux user's password. If macOS has no Homebrew, Linux has no `apt-get`, or the automatic install fails, the install command stops before touching any OffiCraft files and prints the commands to install what is missing. The install command only checks that tmux is installed, not its version.
- curl (a tool for downloading files): built into macOS and most Linux distributions
- At least one of Claude Code or Codex, already signed in

### Windows: preparing WSL2

On Windows, OffiCraft runs in Ubuntu inside WSL2. macOS and Linux users can skip this section.

1. Open PowerShell as administrator and run:

   ```powershell
   wsl --install -d Ubuntu
   ```

2. The first WSL install requires a restart. Then open "Ubuntu" from the Start menu and set the Ubuntu user name and password as prompted.

Every command after this runs in the Ubuntu window, which you open by clicking "Ubuntu" in the Start menu.

### Step 1: Install and sign in to Claude Code or Codex

Install whichever tool you use, or both.

Claude Code:

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

Once it is installed, open a new terminal window, run `claude`, and finish signing in in the browser as prompted.

Codex:

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

Once it is installed, open a new terminal window, run `codex`, and choose "Sign in with ChatGPT" to sign in.

Check: run the matching command. If it shows a version number and sign-in information, the tool is installed and signed in.

```bash
claude --version && claude auth status --text
```

```bash
codex --version && codex login status
```

### Step 2: Install OffiCraft

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

The installer first checks the system requirements and automatically installs tmux if it is missing. If anything is still missing, it stops and lists the tools you need to install.

### Step 3: Open the setup link

When the install finishes, the terminal prints the following message and setup link:

```
[install] ✅ OffiCraft is installed and running in the background (…)

  open:    http://127.0.0.1:7755/?code=…
```

Open the link in a browser and set a sign-in password to enter the console. Windows users open it in a Windows browser.

### Confirm the installation

Once you sign in to the console with the password you set, the "Office" page shows the Assistant, Mira, online and able to answer chat messages. That means OffiCraft is installed.

### Install notes

- **Claude Code version**: Claude Code **2.1.287** or later is required. The install script only checks that Claude Code is installed, not its version. If the version is too old, the install still succeeds, but waking Claude members fails, and the Claude column under "Monitor › Machines" is marked "too old".
- **You can also sign in from the console**: once OffiCraft is installed, "Sign in" is in the version-number menu of each machine's Claude and Codex columns under "Machines" in the console's Monitor tab.
- **Codex alone is fine**: once Codex is signed in, the Assistant, Mira, and new members switch to Codex automatically the first time they start. When both are installed, Claude Code is used. If Mira will not start, see "The machine has only Codex, and Mira or a new member will not start" in [Troubleshooting and FAQ](troubleshooting.md).
- **What the install command does**: it downloads the latest OffiCraft and installs it only after confirming the files are complete and intact. If the files are bad, it stops without installing anything. For the full steps, see "What the install script actually does".
- **Running in the background**: on macOS and Linux, OffiCraft keeps running in the background, does not stop when you close the terminal, and starts automatically after the computer restarts. For Windows (WSL2), see "Windows (WSL2)" below.
- **If the install fails**: the terminal prints the reason. For an explanation of each reason, see "Installing" in [Troubleshooting and FAQ](troubleshooting.md).
- **Using it for the first time**: see [Your first office](quickstart.md).

---

## Linux

Installing, upgrading, and uninstalling on Linux use the same commands as on macOS. The difference is that background services are managed by systemd. systemd is the system component most Linux distributions use to start and stop background services, and it does the same job as launchd on macOS. OffiCraft registers systemd user services: background services that belong only to the currently signed-in user, need no administrator rights, and are controlled with `systemctl --user`.

| Item | macOS | Linux |
| --- | --- | --- |
| Manages background services | launchd | systemd user services (`systemctl --user`) |
| Server background service | `com.officraft.serve` | `com.officraft.serve.service` |
| Warden background service | `com.officraft.ocwarden` | `com.officraft.ocwarden.service` |
| Background service config location | `~/Library/LaunchAgents/` | `~/.config/systemd/user/` |
| Server log | `~/.officraft/server/log/serve.log` | Same |
| Warden logs | `~/.officraft/warden/log/ocwarden.out.log`, `ocwarden.err.log` | Same |

- **Keeps running after you sign out**: by default, systemd user services stop once the user signs out. The install automatically runs `loginctl enable-linger` to turn on linger (the setting that keeps a user's background services running after sign-out and starts them at boot), so OffiCraft keeps running in the background after you sign out or restart.
- **Background service status**: `systemctl --user status com.officraft.serve.service` shows whether the server's background service is running; `journalctl --user -u com.officraft.serve.service` lists the starts, stops, and abnormal exits systemd recorded. The server's own log is still in the file listed in the table above. For the warden, replace `serve` with `ocwarden` in the commands.

---

## Windows (WSL2)

OffiCraft does not run directly on Windows. On Windows, it is installed inside WSL2. WSL2 is a built-in Windows feature that runs a complete Linux inside Windows; OffiCraft is installed in WSL2's Ubuntu (a Linux distribution) and is used the same way as on Linux. For the install steps, see "Windows: preparing WSL2" and Steps 1–3 above.

### Turning on systemd

In WSL2, OffiCraft also runs as systemd user services. The current Ubuntu installed by `wsl --install` has systemd turned on by default; other distributions and older Ubuntu versions do not start systemd by default. **When systemd is not running, the install script stops and asks you to do this step.**

In the Ubuntu window, run the following command to add the two lines `[boot]` and `systemd=true` to `/etc/wsl.conf`:

```bash
printf '[boot]\nsystemd=true\n' | sudo tee -a /etc/wsl.conf
```

WSL must be version 0.67.6 or later; older versions do not support systemd. Check the version with the following command, and update with `wsl --update` if it is too old:

```powershell
wsl --version
```

Then run the following command in Windows PowerShell to shut down WSL2, and reopen the Ubuntu window for the setting to take effect:

```powershell
wsl.exe --shutdown
```

> [!WARNING]
> `wsl.exe --shutdown` shuts down every WSL2 Linux running on this Windows computer, and any OffiCraft and members already running inside them stop too.

### Things to know

- **Install and sign in to Claude Code or Codex inside Ubuntu.** OffiCraft cannot find or use a Claude Code or Codex installed on the Windows side.
- **Open the setup link in a Windows browser.** By default, WSL2 forwards connections to `127.0.0.1` on Windows into WSL2, so the `http://127.0.0.1:7755/?code=…` link printed when the install finishes can be opened directly in a Windows browser.
- **Members stay online after every Ubuntu window is closed.** During the install, OffiCraft puts a file that keeps WSL2 running in the Windows "Startup" folder and runs it once right away, so Windows keeps Ubuntu running after all windows are closed. Each time you sign in to Windows, the file in the "Startup" folder runs again. If the install prints `WARN: WSL keep-alive not set up`, the file was not set up, and members go offline once every Ubuntu window is closed. For the file's contents, a manual alternative, and how to stop it, see "On Windows (WSL2), do members go offline when I close the terminal?" in [Troubleshooting and FAQ](troubleshooting.md).

### Several Ubuntu entries in the Start menu

1. Run the following command in PowerShell to see the name of each Ubuntu:

   ```powershell
   wsl -l -v
   ```

2. Run the following command with each name in turn; the one that shows `.officraft` is the Ubuntu OffiCraft is installed in:

   ```powershell
   wsl -d <name> --cd ~ -- ls -d .officraft
   ```

3. From then on, open that Ubuntu with the following command:

   ```powershell
   wsl -d <name>
   ```

---

## Upgrading

Upgrading is done in the console under **Settings › System update & backup**; you do not need to run the install command again.

- **Check for updates**: asks GitHub (the site where OffiCraft publishes new versions) whether there is a new version. When there is, "Update to latest" appears.
- **Update to latest**: OffiCraft downloads the new version, confirms the files are complete, and restarts automatically. The console page reloads automatically, and once it succeeds, "Current version" changes to the new version number.
- **Automatic updates**: when this is on, OffiCraft upgrades automatically as soon as it detects a new version. It is on by default for new installs (upgrading only to official releases); existing installs keep their previous setting.

OffiCraft on other machines follows automatically, usually within 15 minutes, with no need to do anything on each machine.

"The update check failed" usually means GitHub cannot be reached, typically because of the network or a proxy (an intermediary server common on company networks).

For what each field such as "Receive beta versions" and "Update check interval" means, see "Advanced: upgrade settings".

---

## Uninstalling

The uninstall command:

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

This stops OffiCraft and moves the programs, database, and background service configuration to a `~/.officraft.bak-<time>` folder. **By default, no data is deleted.** At the end, the script prints a restore command; running it brings everything back.

Whether the uninstall succeeded is determined by the completion message the script prints at the end.

Uninstalling does not touch the members' workspaces (`~/.officraft/agents/`), and does not remove the warden (the background program that starts and stops members) on this computer. For the options that also remove the warden or permanently delete data, see "Advanced: other uninstall options".

---

## Adding or removing machines

### Adding a second machine

**The other computer has to be able to reach the console on this computer.** The install command copied from the console contains the address you used to open the console. If you opened the console through `localhost` (the address meaning "this computer itself"), the copied command, run on another computer, connects back to that other computer itself and cannot reach this console. Only a command copied while the console was opened through an address the other computer can reach (for example, a forwarded address) works. To set up forwarding, tell Mira "I want to reach the console from somewhere other than this computer" (see [Use it on your phone](mobile.md)); for the protections to put in place before opening access, see [Protect your studio](security.md).

The screens and commands used to add a machine:

- **Add machine / onboard**: under "Machines" in the Monitor tab, enter a machine name to create a machine record. A newly created machine has nothing installed yet.
- **Install**: "Install" in the machine name menu opens the "Install machine" window. For each operating system (macOS, Linux, Windows (WSL2)), the window lists the system requirements, the WSL2 preparation for Windows, where to paste the commands, and the install command for this particular machine. The copy button to the right of each command (an icon only; hovering shows "Copy") copies exactly that command as shown. The install command can only be used once, and it also expires after 10 minutes. After 10 minutes, the text below the command says "This command has expired. Regenerate it.", and the copy button becomes a regenerate button (↻; hovering shows "Regenerate command") that replaces it with a new command. The window does not show whether a command has already been used: running a used command again fails, and closing and reopening the window then produces a new command.
- **Install command**: run it in a terminal on the other computer, which can be a Mac, Linux, or WSL2 on Windows. The command first checks for `tmux` and `curl` (a tool for downloading files, built into macOS). If `tmux` is missing, computers with Homebrew install it with Homebrew, and Linux and WSL2 install it with `apt-get`; before installing, it warns that you may need to enter your Linux user's password. If `tmux` cannot be installed or another tool is missing, it stops and prints the command to install it, which on Linux is `sudo apt-get install -y <tool name>`. Unsupported computers are refused at this step. Once the install finishes, that machine appears in the "Machines" list with the status "Online".

That computer also has to meet the "System requirements" (`tmux` 3.0 or later; Claude Code 2.1.287 or later, or Codex instead) and be signed in on that computer. On Linux and WSL2, the warden runs as a systemd user service; see "Linux". Once the machine is online, you can assign members to work on that computer in Member details, and members can help each other across machines.

### Removing a machine from the console

Under Monitor › Machines, the machine name menu has two removal options:

- **Uninstall**: has that machine remove OffiCraft from itself. Once it succeeds, the machine goes offline and its record is kept, so you can "Install" it again later. Only online machines can be uninstalled; if members on that machine are still online, a warning appears first.
- **Delete**: removes the machine from the list. That machine's sign-in credential stops working immediately, and members on that machine are disconnected. The programs on the machine are not removed. **Deleting cannot be undone**; the only way back is to reinstall.

Fully removing a machine needs both, in this order: uninstall first, then delete.

---

## Advanced: install options

Options go after `bash -s --`, for example `curl -fsSL … | bash -s -- --tag v0.4.1`.

| Option | Purpose |
| --- | --- |
| `--tag <version>` | Install a specific version (the `OC_INSTALL_TAG` environment variable works too). |
| `--force` | Overwrite the files of an existing install. |
| `--restart-live` | Allow restarting OffiCraft services that are currently running. |
| `--relocate` | Allow moving existing services to a different port or configuration. |
| `--namespace <name> --port <port>` | Install a second, independent studio on the same computer. |
| `--foreground` | Run the service in the foreground of the current terminal, stopped with Ctrl-C, without registering a background service. |

> [!WARNING]
> If OffiCraft is running on this computer, reinstalling restarts the service. During the restart, every open console and every member is disconnected; no data is lost. If OffiCraft is already installed on this computer, the install script warns first and asks for confirmation. When run as `curl … | bash`, there is no way to answer the confirmation, so you have to add `--force` to continue. `--force` only allows overwriting files, not restarting; to restart, also add `--restart-live`:
>
> ```bash
> curl -fsSL … | bash -s -- --force --restart-live
> ```

A manual install uses the `officraft-<tag>-<os>-<arch>.tar.gz` archive from [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases); unpack it and run the `./install.sh` inside. Pick the one for your computer: `darwin-arm64` for a Mac, `linux-amd64` for x86_64 Linux (which is also what WSL2 is on most Windows computers), and `linux-arm64` for arm64 Linux.

> [!NOTE]
> If `claude` was installed with a tool that manages several program versions, such as asdf, nvm, or volta, background services may not find `claude`. When the install script cannot find Claude Code or Codex, the environment variables `OC_CLAUDE_BIN=/absolute/path/claude` or `OC_CODEX_BIN=/absolute/path/codex` tell the install script where the program is; once set, running the install again uses the specified location. Running the install again does not affect existing data.

### What the install script actually does

1. Confirms the computer is supported: macOS Apple Silicon (darwin/arm64), Linux x86_64 (amd64), or Linux arm64. If not, it stops before downloading anything. On Linux, it also confirms systemd is running.
2. Confirms this computer has `tmux` and at least one of `claude` or `codex`. It checks these two even if this computer only hosts the server and all members run elsewhere. If `tmux` is missing, it first installs `tmux` with Homebrew or `apt-get`, warning before an `apt-get` install that you may need to enter your Linux user's password. If something is still missing, it stops before touching any files and prints the commands to install it. The install script only checks that the tools are installed, not their versions: with Claude Code older than 2.1.287 the install still succeeds, but waking Claude members fails, and the Claude column under "Monitor › Machines" is marked "too old". There are two ways to upgrade Claude Code: "Upgrade Claude Code" in the same version-number menu, or running `npm install -g @anthropic-ai/claude-code` on that computer.
3. Downloads the `officraft-<tag>-<os>-<arch>.tar.gz` matching this computer and `checksums.txt` from the latest release, and uses the sha256 checksum to confirm the downloaded file is exactly what was published. It continues only if the check passes; if verification fails, it stops without installing anything.
4. If OffiCraft is already installed on this computer, warns first and asks for confirmation.
5. Installs `ocserverd`, `ocwarden`, `ocagent`, and `officraft` into `~/.officraft/bin` and upgrades the database structure, keeping existing data. For what each of these programs does, see [Architecture and how it works](architecture.md).
6. Confirms the port is not in use (7755 by default).
7. Registers the background service `com.officraft.serve`: handed to launchd on macOS, and registered as the systemd user service `com.officraft.serve.service` with linger turned on, on Linux. The service runs in the background, does not stop when you close the terminal, and starts automatically at boot.
8. Prints the one-time setup link `http://127.0.0.1:7755/?code=…`.
9. Once the password is set, the server automatically installs this computer's warden and wakes the Assistant, Mira. If one of those steps fails, an "Automatic setup did not finish" banner appears at the top of the console, stating the reason. On a new station where no language has been chosen yet, the server also asks Mira to greet you in Chinese and English and to open a reply card asking which language you want; your answer sets the console's language and the language every member writes to you in.

Official releases are prebuilt programs; this computer does not need Go, node, or python.

---

## Advanced: a second studio on the same computer

```bash
./install.sh --namespace lab --port 7756
```

| Item | `--namespace lab` | Main studio (without `--namespace`) |
| --- | --- | --- |
| Data directory | `~/.officraft-lab` | `~/.officraft` |
| Background service | `com.officraft.serve.lab` | `com.officraft.serve` |
| Config file | `~/.officraft-lab/server/oc.toml` | `$OC_CONFIG` or `./oc.toml` in the current directory |
| Database | `~/.officraft-lab/server/data/` | `~/.officraft/server/data/` |
| Warden background service | `com.officraft.ocwarden.lab` | `com.officraft.ocwarden` |

- On Linux, background service names end with an extra `.service`, for example `com.officraft.serve.lab.service`.
- The name may only contain lowercase English letters, digits, and `-`, and is at most 16 characters long; otherwise the install stops with an error. You can also set it with the `OC_NAMESPACE` environment variable.
- `--port` is required, and it cannot be the same port as the main studio.
- The install script refuses to change the port of a studio that already exists; the port can only be changed by editing that studio's `oc.toml`.
- Changing `$HOME` does not isolate a second studio: the background service names do not change with it, and reinstalling still restarts the main studio's service. Use `--namespace` to isolate a second studio.

---

## Advanced: upgrade settings

Each field under **Settings › System update & backup**:

- **Current version**: the version this server is running, with "Check for updates" and "View release" next to it. A status of "Latest version not confirmed yet" means the server has not successfully checked GitHub since it started; it does not mean it is already on the latest version.
- **Update to latest**: the server downloads the new version, verifies it with the sha256 checksum attached to the release, and confirms the new program runs before replacing itself and restarting.
- **Automatic updates**: once on, the server upgrades and restarts automatically in the background when it detects a new version. New installs turn it on the first time the password is set with the claim code (upgrading only to official releases, unless "Receive beta versions" below is also on); existing installs keep their previous setting; installs from source keep it off. When an automatic upgrade fails, each retry waits longer than the last (starting at 5 minutes and doubling each time, up to 6 hours); it starts over immediately when a newer version is published or you press "Update to latest".
- **Receive beta versions**: once on, checking for updates also includes pre-release versions on GitHub. Off by default.
- **Update check interval**: how often the background check with GitHub runs, 1–60 minutes, 5 minutes by default. Pressing "Check for updates" and upgrading always check right away, regardless of this field.
- **Let the assistant tell me about new-version features**: when this is on, after upgrading to an official release that has a feature announcement, the Assistant tells you in chat what the version adds. One upgrade produces at most one message; when several versions are skipped, only the latest five are described, and the rest come with a link to the GitHub release list. On by default. Versions upgraded to while it is off are not announced later, even after you turn it back on. If the Assistant is not online, the message waits until the Assistant next comes online. On installs with "Receive beta versions" on, if the running version is promoted to an official release later, the announcement is made at the next restart.

After the server upgrades, the warden and ocagent on each machine update automatically. The new warden and ocagent are included in the server; each warden checks with the server every 15 minutes, and also checks immediately when it reconnects to the server or receives an instruction from it. When it finds a new version, it updates itself, usually within 15 minutes.

---

## Advanced: other uninstall options

Uninstalling leaves the following untouched:

| Left in place | Description |
| --- | --- |
| `~/.officraft/agents/` | The workspace of each member on this computer. |
| `~/.officraft/warden/` | The warden and its own background service `com.officraft.ocwarden`, which keep running after the uninstall. |
| The `~/.officraft` folder itself | Not deleted. |

There are two ways to remove this computer's warden:

- Before uninstalling the server, choose "Uninstall" in the machine name menu under "Monitor › Machines" in the console.
- Run `~/.officraft/warden/ocwarden teardown --canonical` on that computer. For a second studio's warden, use `OC_NAMESPACE=<name> ~/.officraft-<name>/warden/ocwarden teardown` instead.

When the warden is removed on WSL2, the file in the Windows "Startup" folder that keeps WSL2 running is deleted too; a `sleep infinity` already running in the background lasts until you sign out of Windows or restart.

Other uninstall options:

```bash
curl -fsSL … | bash -s -- --uninstall --dry-run          # only lists what it would do, changes nothing
curl -fsSL … | bash -s -- --uninstall --purge --yes      # deletes permanently, including the database; cannot be undone
curl -fsSL … | bash -s -- --uninstall --namespace lab    # removes only the second studio
```

When run as `curl … | bash`, there is no way to answer the `--purge` confirmation, so `--yes` must be added for anything to be deleted; without `--yes`, nothing is deleted. If you save the script to a file and run it, you can answer the confirmation normally.

When an older install script exits early, it may print an extra red line `curl: (23)` or `curl: (56)` at the end. That line does not mean the uninstall failed; the completion message the script prints is still what counts.

---

## Advanced: installing from source

This route is for people who want to modify OffiCraft's code themselves. The official release and an install from source produce different things, and you can only use one of the two. **Installing from source currently supports only macOS**: `bin/ocserver install` only registers launchd background services; on Linux and WSL2, use the official release.

On an install from source, **Automatic updates** stays off by default: if you turn it on, OffiCraft replaces the program you built yourself with the official release when it detects a new version.

Additional requirements:

| Requirement | Minimum version | Purpose |
| --- | --- | --- |
| Go | 1.26 | Compiles the server, warden, and CLI (programs operated by typing commands in a terminal) |
| node, npm | node 18 | Compiles the console front end |
| python3 | 3.11 | Generates and checks `oc.toml` during install, and runs development scripts; without 3.11 or later, `bin/ocserver install` stops |
| `cloudflared` | — | Only needed to open an outward tunnel (a channel that lets the outside network connect back to this computer) |

The install command, run from the root of the repo (the code folder):

```bash
bin/ocserver install
```

When it finishes, it prints a one-time **claim code**. The claim code is used on the console's "Set the admin password" screen and sent together with the password; once the password is set, the claim code stops working immediately.

```bash
bin/ocserver install --dry-run                       # only lists what it would do, changes nothing
bin/ocserver install --force                         # reruns every step, leaves the existing password alone
bin/ocserver install --namespace <name> --port <port>   # installs a second studio on the same machine
```

The default port is also 7755, and can be changed in `oc.toml` or with the `OC_SERVE_PORT` environment variable.

All files for this route are in `~/.officraft/server/`:

```
~/.officraft/server/
  repo/     code
  data/     database (all of the studio's data)
  oc.toml   local settings (port, database location); not overwritten on reinstall
  log/      logs
```

It also registers three background services that start automatically at boot:

| Background service | Purpose |
| --- | --- |
| `com.officraft.serve` | The server itself |
| `com.officraft.autodeploy` | When the git remote has new code, pulls, compiles, and restarts automatically |
| `com.officraft.tunnel` | The cloudflared outward tunnel; skipped automatically when the machine has no cloudflared configuration |

`oc.toml` accepts `[server]` (`port`, `namespace`) and `[storage]` (`dsn`). A misspelled field or an unknown section makes the server refuse to start, so a setting never silently stops working.

Uninstalling:

```bash
bin/ocserver uninstall             # removes the background services and ~/.officraft/server, keeps data/ and oc.toml
bin/ocserver uninstall --purge     # deletes everything, including the database and password (asks for confirmation; --yes skips it)
bin/ocserver uninstall --dry-run   # only lists what it would do, changes nothing
```

---

## Related pages

- [Your first office](quickstart.md)
- [Protect your studio](security.md)
- [Settings and parameters](settings.md)
- [Troubleshooting and FAQ](troubleshooting.md)
