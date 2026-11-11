# Zefxi

Zefxi bridges a Telegram supergroup to one Discord channel

This project exists because [Matterbridge wasn't good enough for me.](#features-comparison-20261009)

## Features

- Reactions
- Reply chains
- Message edits
- Spoof username and profile picture on discord side (replies look like they were sent by the user)
- Auto-matching user accounts with the same usernames
- Linking user accounts manually with optional many-many relations (2 telegram accounts will show up as 1 discord user)
- Markdown formatting including spoilers and quotes
- GIFs, stickers, Telegram Premium emojis are properly converted and show up correctly
- Pinned messages and unpins
- Message deletions
- System events (joins, leaves, chat picture change)

## Setup

1. Create a Telegram bot
    - add it to the group as an administrator (required to recieve reaction updates)
    - disable its privacy mode in BotFather so it receives group messages
2. Create a Discord bot
    - Enable the **Message Content** and **Server Members** privileged intents.
    - Add bot to the server, and give it access to the channel, including manage webhooks, manage messages, send messages, read message history, add reactions and attach files.
3. Obtain a Telegram API ID and hash from [my.telegram.org](https://my.telegram.org/), and join the supergroup with the user account.
4. Copy `.env.example` to `.env` and fill in all your info.
5. Run with nix `nix run github:sleroq/zefxi`, or with docker. For the first login with Docker, use `docker compose run --rm zefxi` interactively. Scan the QR code and enter the account's 2FA password if prompted. The user session is stored alongside the database; keep it private and persistent.

### Runtime dependencies for running outside of nix and docker

- [LottieConverter](https://github.com/sot-tech/LottieConverter)
- [ffmpeg v9+](https://ffmpeg.org/download.html)

## Features and limitations

- Only underlying emoji's of Telegram Premium custom reactions are forwarded to Discord side
- Multiple reactions from on the Discord side will result in just one reactions on Telegram. But you can check detailed info about who liked with `/reacts` command
- Commands directed to the bridge bot and its replies are not forwarded
- Files over the configured Discord upload limit won't be bridged to the Discord side, no fallback is provided here

## Features comparison (2026.10.09)

| Feature                   | Matterbridge                                    | zefxi                                               |
| ------------------------- | ----------------------------------------------- | --------------------------------------------------- |
| Topology                  | Multiple channels and services                  | One Telegram chat ↔ one Discord channel             |
| Text                      | ✔️                                              | ✔️                                                  |
| Edits                     | ✔️                                              | ✔️                                                  |
| Deletes                   | Discord → Telegram only                         | ✔️                                                  |
| Replies                   | ✔️                                              | ✔️                                                  |
| Sender identity           | Names and avatars                               | Names and avatars; cross-platform accounts linking  |
| Mentions                  | Mostly translated to readable names             | ✔️                                                  |
| Rich text                 | Partial                                         | ✔️                                                  |
| Telegram media → Discord  | Files, photos, video, audio, voice and stickers | Same, plus animations and video notes               |
| Discord media → Telegram  | Attachment URLs                                 | Native media uploads; URL fallback                  |
| Stickers and custom emoji | Only Telegram sticker conversion                | Both sides stickers and telegram emoji's conversion |
| Reactions                 | -                                               | Aggregated bot reactions both ways                  |
| Pins                      | -                                               | ✔️                                                  |
| System events             | Join/leave events                               | Join/leave and selected chat events                 |
| Telegram topics           | Supported for routing                           | -                                                   |
