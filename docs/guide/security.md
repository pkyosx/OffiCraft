# Protect your studio

After reading this page, you can control who gets into the console, prepare before opening access from your phone or from outside, and know which keys and files you need to keep safe.

Behind the console is your entire studio: members have wide permissions on your computer, and the Secrets tab holds the account keys you have handed to members. So set up how you sign in to the console first, and only then think about connecting from outside.

---

## Before opening access from outside

By default, the console can only be opened on the computer OffiCraft is installed on; phones and other computers cannot connect directly. To use it from a phone or another computer, tell Mira "I want to reach the console from somewhere other than this computer". See [Use it on your phone](mobile.md).

**Before forwarding is turned on, do these three things first:**

1. Make sure your sign-in password is hard to guess. Once outside access is open, other people can reach the sign-in page too.
2. Turn on two-factor authentication. A passkey does not replace two-factor authentication: even with a passkey, you can still sign in with only the password.
3. Shorten the session length if needed.

**Do not open access from outside without HTTPS.** Tunnel tools such as cloudflared give you a public HTTPS address, and anyone who knows the address can open the sign-in page. The forwarding feature of private networks such as Tailscale (for example `tailscale serve`) can only be reached by your own devices. Tailscale Funnel, on the other hand, makes the address public, and anyone who knows it can open the sign-in page.

Once a public address is open, your webhook endpoint addresses are exposed along with it. A webhook address needs no sign-in; anyone who has it can send events to a member. So only enter webhook addresses in the services you are connecting. For what to do if an address leaks, see [Send outside events to members automatically (webhooks)](webhook.md).

---

## Sign-in password

The password you set the first time you opened the console is the password for signing in to the console. It is at least 8 characters long.

**Changing your password**: click your profile picture in the top-right corner › Account security › Change password. After you change it, the console is signed out on every other device that was signed in, and you have to sign in again with the new password.

If you forget your password, you reset it by running a command on the computer OffiCraft is installed on. For the steps, see "Forgot your password, lost your authenticator, or a passkey stops working" in [Troubleshooting and FAQ](troubleshooting.md).

---

## Two-factor authentication

Once it is on, every password sign-in also asks for the 6-digit code shown in an authenticator app (for example Google Authenticator, 1Password, or the Passwords app built into iPhone). Turning it on is recommended if the console will be reached from outside.

To turn it on: click your profile picture in the top-right corner › Account security › Two-factor authentication, scan the QR code with your authenticator app as the screen shows, then enter the code.

**If you lose your authenticator**, you turn two-factor authentication off by running a command on the computer OffiCraft is installed on. For the steps, see "Forgot your password, lost your authenticator, or a passkey stops working" in [Troubleshooting and FAQ](troubleshooting.md).

For a description of the settings screen, see "Account security and sign-in" in [Interface reference](interface.md).

---

## Passkeys

A passkey lets you sign in with Face ID, Touch ID, a fingerprint, your device passcode, or a physical security key, without typing your password or a two-factor code. Signing in with your password still works.

To add one: click your profile picture in the top-right corner › Account security › Passkeys, give it a device name you will recognize (for example "My laptop"), then finish in the dialog the browser shows.

After that, the sign-in page shows "Sign in with a passkey".

**Each passkey only works on the address it was added on.** If you open the console at different addresses (for example the forwarded HTTPS address and `http://localhost:7755`), add a passkey on each address separately. Passkeys cannot be used when you open the console through an IP address such as `http://127.0.0.1:7755`; open it through the `localhost` address instead. Both addresses open the same studio.

If you lose a device, delete that device's passkey from the same list.

For a description of the settings screen, see "Account security and sign-in" in [Interface reference](interface.md).

---

## Session length

How long you stay signed in before you have to sign in again is set in **Settings › Parameters › Session length**. You can choose 12 hours, 24 hours, 7 days, or 30 days; the default is 24 hours. A new setting takes effect from your next sign-in.

If the console is reached from outside, or you sign in on devices other people can also use, a shorter setting is recommended.

---

## Secrets and vault.key

The values in the Secrets tab, and the content of Secret reply cards that has not yet been deleted, are encrypted with a file called `vault.key` before being stored in the database. `vault.key`, the database, and the automatic backups all sit in the same folder on the same computer, and normally you do not need to deal with them. Restoring from a backup on the same computer does not require touching `vault.key` either.

You only need to be careful when you copy a database backup to another computer (for example, when you change computers): backups do not include `vault.key`, so take `vault.key` along with them. Otherwise the existing secrets cannot be read and can only be deleted and created again. Leaving `vault.key` out of backups is deliberate: someone who gets hold of only a backup cannot decrypt the secrets inside it.

For the steps to back up and restore the database, see "Backup and restore" in [Troubleshooting and FAQ](troubleshooting.md). For adding, editing, and deleting secrets, and how members fetch them, see [Give members your account keys (secrets)](secrets.md).

---

## Signing keys

Signing keys are what OffiCraft uses to prove that sign-ins and share links are valid. Your sign-in, the sign-ins of members and machines, and the links created by "Copy external link" and "Copy share link" are all checked against a signing key.

If you suspect a sign-in or a share link has leaked:

1. Go to the "Signing keys" card at the bottom of **Settings › System update & backup** and press "Create a new key". The new key takes over immediately, and nobody who is signed in is signed out.
2. Wait until every machine has come online at least once, so that it switches to the new key.
3. Press "Remove" next to the old key.

**Removing cannot be undone and takes effect immediately**: every sign-in and share link signed with the old key stops working, and people holding those links are not notified. Machines that have not yet switched to the new key are disconnected immediately. So before removing, make sure every machine has come online.

For a description of the Signing keys card, see "System update & backup" in [Settings and parameters](settings.md). For how long members and machines each stay signed in, see "Parameters" in [Settings and parameters](settings.md).

---

## Related docs

- [Use it on your phone](mobile.md)
- [Give members your account keys (secrets)](secrets.md)
- [Send outside events to members automatically (webhooks)](webhook.md)
- [Settings and parameters](settings.md)
- [Troubleshooting and FAQ](troubleshooting.md)
