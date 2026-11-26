# Give members your account keys (secrets)

This page explains what a secret is, what each secret contains, and how a member asks you for a secret that is not in the list.

## Secrets

A secret is a confidential value you store in OffiCraft, such as an API token or sign-in key for some service. Members fetch it when they need it, and the value never appears in chat, reply cards, or task records. You manage secrets in the Secrets tab on the top navigation bar.

Only you can add, edit, and delete secrets. Every active member, staff and outsource alike, can fetch them.

**Do not paste a value straight into chat.** Chat content stays in the records and is also carried into members' memory.

## What each secret contains

- **Name**: for you and members to recognize it.
- **Value**: the confidential value itself. **Once saved, the console never shows it again**, and even you cannot read the original value back. You can replace it with a new value.
- **Note**: limits on how it may be used, for example "Only for sending the newsletter; do not use it for anything else." Members read the note before using the secret.
- **Secret ID**: a code starting with `sec-`. Giving a member the secret ID is the clearest way to assign work, for example "Sign in to the newsletter service with secret sec-aaaaaaaaaaaa and send this week's newsletter." Without a secret ID, a member finds the matching secret by its name and note; if it cannot tell which one, it opens a reply card asking you to specify.
- **Last accessed**: the last time a member fetched the secret. Use it to confirm a member used the secret you told it to, or to find secrets nobody has used for a long time and that can be deleted. If a secret that should not be in use was fetched recently, revoke it at the matching service and issue a new key, then come back to OffiCraft and update the value.

Deleting a secret **cannot be undone**. Any later attempt by a member to fetch it by that secret ID fails.

## When a member needs a secret that is not in the list

The member opens a Secret reply card to ask you for it. You fill in the value on the card; after you send it, it is not shown again, and only the member who opened the card can fetch it. You can also reply with a message below the card, which ends the card. The content is deleted after a while; for how long, see "Parameters" in [Settings and parameters](settings.md).

## Take vault.key with you when you change computers

Secret values are encrypted with a file called `vault.key`. Normally you do not need to deal with it. Only when you move a database backup to another computer do you need to take `vault.key` along, because backups do not include `vault.key`. For details, see "Secrets and vault.key" in [Protect your studio](security.md).

For a description of each field, see "Secrets" in [Interface reference](interface.md). For how members fetch secrets, see "How members fetch secrets" in [Architecture and how it works](architecture.md).

## Related pages

- [Protect your studio](security.md)
- [Troubleshooting and FAQ](troubleshooting.md)
- [Assigning work and answering reply cards](daily.md)
- [Interface reference](interface.md)
