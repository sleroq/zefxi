# Zefxi - Telegram to Discord Bridge

A high-fidelity Telegram to Discord bridge written in Zig, designed to seamlessly sync messages, reactions, and user interactions between Telegram supergroups and Discord channels.

## Features

- 🔄 **Bidirectional Message Sync**: Forward messages between Telegram supergroups and Discord channels
- 👤 **Fake Discord Profiles**: Create Discord webhook profiles for each Telegram user for authentic representation
- ❤️ **Reaction Mirroring**: Sync reactions between platforms in real-time
- 💬 **Reply Threading**: Preserve reply chains and message context across platforms
- 🔐 **Full Authentication**: Complete Telegram and Discord bot authentication flows
- ⚡ **Real-time Sync**: Instant message forwarding with minimal latency
- 🎯 **Supergroup Focus**: Optimized for Telegram supergroups to Discord channel bridging

## Roadmap

- ✅ TDLib JSON interface integration
- ✅ Telegram authentication flow
- ✅ Real-time message receiving
- 🚧 Discord bot integration
- 🚧 Webhook-based fake user profiles
- 🚧 Bidirectional message forwarding
- 🚧 Reaction synchronization
- 🚧 Reply chain preservation
- 🚧 User avatar and name caching
- 🚧 Message editing sync
- 🚧 File/media forwarding

## Prerequisites

Install Nix package manager and use the provided development environment:

```bash
nix develop
```

## Setup

1. **Get your Telegram API credentials from [my.telegram.org](https://my.telegram.org/apps)**

2. **Create a Discord bot at [Discord Developer Portal](https://discord.com/developers/applications)**

3. **Configure environment variables:**
   ```bash
   export TELEGRAM_API_ID="your_api_id"
   export TELEGRAM_API_HASH="your_api_hash"
   export TELEGRAM_CHAT_ID="supergroup_id_to_bridge"
   export DISCORD_BOT_TOKEN="your_discord_bot_token"
   export DISCORD_CHANNEL_ID="channel_id_to_bridge"
   ```

4. **Run the bridge:**
   ```bash
   nix build
   ./result/bin/zefxi
   ```

## Core Architecture

- **Telegram Integration**: TDLib JSON interface for robust Telegram connectivity
- **Discord Integration**: Discord bot API with webhook support for fake profiles
- **Message Pipeline**: Efficient bidirectional message processing and transformation
- **User Management**: Dynamic Discord webhook creation and caching for Telegram users
- **Reaction Engine**: Real-time reaction synchronization between platforms

## License

MIT License
