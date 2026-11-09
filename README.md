# Zefxi

Zefxi bridges Telegram group to one Discord channel

This project exists because [Matterbridge wasn't good enough for me.](#Features-comparison)

## Features

- Reactions
- Reply chains
- Message edits
- Spoof username and profile picture on discord side (replies look like they were sent by the user)
- Auto-matching user accounts with the same usernames
- Linking user accounts manually with optional many-many relations (2 telegram accounts will show up as 1 discord user)
- Markdown formatting including spoilers and quotes
- GIFs, stickers, Telegram Premium emojis are properly converted and show up correctly
- Pinned messages (though deleting unpinning is not synced to discord)
- System events (joins, leaves, chat picture change)

## Setup

1. Create a Telegram bot
    - add it to the group as an administrator (required to recieve reaction updates)
    - disable its privacy mode in BotFather so it receives group messages
2. Create a Discord bot
    - Enable the **Message Content** and **Server Members** privileged intents.
    - Add bot to the server, and give it access to the channel, including manage webhooks, send messages, read message history, add reactions and attach files.
3. Copy `.env.example` to `.env` and fill in all your info
4. Run with nix `nix run github:sleroq/zefxi`, or with docker.

### Runtime dependencies for running outside of nix and docker

- [LottieConverter](https://github.com/sot-tech/LottieConverter)
- [ffmpeg v9+](https://ffmpeg.org/download.html)

## Features and limitations

- Only underlying emoji's of Telegram Premium custom reactions are forwarded to Discord side
- Multiple reactions from on the Discord side will result in just one reactions on Telegram. But you can check detailed info about who liked with `/reacts` command
- Messages of any bot won't be forwarded both directions, the reason is - telegram bots can't see each other unless explicitly tagged or replied
- All bot commands are intentionally not forwarded to the discord
- Files over 20MB won't be bridged to the Discord side, no fallback is provided here

## Features comparison (2026.10.09)

| Feature                   | Matterbridge                                    | zefxi                                                        |
| ------------------------- | ----------------------------------------------- | ------------------------------------------------------------ |
| Topology                  | Multiple channels and services                  | One Telegram chat ↔ one Discord channel                      |
| Text                      | ✔️                                              | ✔️                                                           |
| Edits                     | ✔️                                              | ✔️                                                           |
| Deletes                   | Discord → Telegram only                         | Discord → Telegram only                                      |
| Replies                   | ✔️                                              | ✔️                                                           |
| Sender identity           | Names and avatars                               | Names and avatars; cross-platform account linking            |
| Mentions                  | Mostly translated to readable names             | Linked accounts receive translated mentions                  |
| Rich text                 | Partial                                         | ✔️                                                           |
| Telegram media → Discord  | Files, photos, video, audio, voice and stickers | Same, plus animations and video notes                        |
| Discord media → Telegram  | Attachment URLs                                 | Native media uploads; URL fallback                           |
| Stickers and custom emoji | Only Telegram sticker conversion                | Discord sticker and limited Telegram custom emoji conversion |
| Reactions                 | -                                               | Aggregated bot reactions both ways                           |
| Pins                      | -                                               | Telegram pins; Discord pins and unpins                       |
| System events             | Join/leave events                               | Join/leave and selected chat changes                         |
| Telegram topics           | Supported for routing                           | -                                                            |
