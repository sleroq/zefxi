# Zefxi - Telegram to Discord Bridge

A high-fidelity Telegram to Discord bridge written in Zig, designed to seamlessly sync messages, reactions, and user interactions between Telegram supergroups and Discord channels.

## Current Status

✅ **Working Bridge**: The bridge is currently functional with basic message forwarding from Telegram to Discord  
⚠️ **Limitations**: Currently missing reply support and user spoofing on Discord side  
🔧 **In Development**: Working towards full bidirectional sync with user spoofing and reply preservation  

## Features

- ✅ **Telegram to Discord Sync**: Forward messages from Telegram supergroups to Discord channels
- ✅ **Discord to Telegram Sync**: Forward messages from Telegram supergroups to Discord channels
- ✅ **Real-time Message Processing**: Instant message forwarding with minimal latency
- ✅ **Telegram Authentication**: Complete Telegram authentication flow with TDLib
- ✅ **Discord Bot Integration**: Basic Discord bot integration for message posting
- ❌ **Reply Threading**: Reply chain preservation not yet implemented
- ❌ **User Spoofing**: Discord webhook profiles for Telegram users not yet implemented
- ❌ **Reaction Mirroring**: Reaction sync not yet implemented

## Roadmap

- ✅ TDLib JSON interface integration
- ✅ Telegram authentication flow
- ✅ Real-time message receiving
- ✅ Message filtering (ignores own messages)
- ✅ Basic Discord bot integration
- ✅ Bridge architecture with threaded clients
- ✅ Telegram to Discord message forwarding
- 🚧 Discord webhook integration for user spoofing
- 🚧 Reply chain preservation and threading
- 🚧 Bidirectional message forwarding (Discord to Telegram)
- 🚧 Reaction synchronization
- 🚧 User avatar and name caching
- 🚧 Message editing sync
- 🚧 File/media forwarding
- 🚧 Full Discord WebSocket integration

## Prerequisites

Install Nix package manager and use the provided development environment:

```bash
nix develop
```

## Setup

1. **Get your Telegram API credentials from [my.telegram.org](https://my.telegram.org/apps)**

2. **Create a Discord bot at [Discord Developer Portal](https://discord.com/developers/applications)**
   - Enable necessary bot permissions for your target channel
   - Get the bot token

3. **Configure environment variables:**
   ```bash
   export TELEGRAM_API_ID="your_api_id"
   export TELEGRAM_API_HASH="your_api_hash"
   export TELEGRAM_CHAT_ID="supergroup_id_to_bridge"  # Optional, monitors all chats if not set
   export DISCORD_TOKEN="your_discord_bot_token"
   export DISCORD_CHANNEL="channel_id_to_bridge"
   export DEBUG="1"                                   # Optional, enables debug logging
   ```

4. **Run the bridge:**
   ```bash
   nix develop --command zig build run
   ```
   
   Or build and run separately:
   ```bash
   nix develop --command zig build
   ./zig-out/bin/zefxi
   ```

## Development

For easier development, copy the example environment file and fill in your values:

```bash
cp .env.example .env
# Edit .env with your actual credentials
```

The `.env` file should contain:
```bash
export TELEGRAM_API_ID="your_api_id"
export TELEGRAM_API_HASH="your_api_hash"
export TELEGRAM_CHAT_ID="supergroup_id_to_bridge"  # Optional
export DISCORD_TOKEN="your_discord_bot_token"
export DISCORD_CHANNEL="channel_id_to_bridge"
export DEBUG="1"  # Optional, enables detailed logging
```

Then source it and run:
```bash
source .env
nix develop --command zig build run
```

Or combine in one line:
```bash
source .env && nix develop --command zig build run
```

**Note**: Make sure to add `.env` to your `.gitignore` to avoid committing sensitive credentials.

## Upcoming Features

The next major development phases will include:

- **User Spoofing**: Discord webhooks to create fake user profiles matching Telegram users
- **Reply Support**: Preserving and recreating reply chains across platforms  
- **Bidirectional Sync**: Discord to Telegram message forwarding
- **Rich Media**: File, image, and other media forwarding
- **Reactions**: Cross-platform reaction synchronization

## Core Architecture

- **Telegram Integration**: TDLib JSON interface for robust Telegram connectivity
- **Discord Integration**: Discord bot API with planned webhook support for user spoofing
- **Message Pipeline**: Efficient message processing and transformation pipeline
- **Threaded Design**: Separate threads for Telegram and Discord clients
- **Future: User Management**: Dynamic Discord webhook creation and caching for Telegram users
- **Future: Reaction Engine**: Real-time reaction synchronization between platforms

## Debugging

Enable debug mode for detailed logging:
```bash
export DEBUG="1"
```

This will show:
- Full message JSON from Telegram (when debug enabled)
- Message processing steps
- Discord API interactions
- User ID tracking and filtering

## License

MIT License
