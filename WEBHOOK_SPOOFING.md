# Discord Webhook Avatar and Username Spoofing

This implementation provides Discord webhook functionality that allows you to send messages with custom usernames and avatars, effectively "spoofing" different users. It's integrated into the Zefxi bridge to make Telegram messages appear in Discord as if they came from the actual Telegram users.

## Features

- **Username Spoofing**: Send messages with any custom username
- **Avatar Spoofing**: Use any avatar URL for your webhook messages
- **Telegram User Integration**: Automatically extract Telegram user info and avatars
- **Rich Message Support**: Send embeds, components, and other rich content
- **Bridge Integration**: Seamlessly integrated with the main Telegram-Discord bridge
- **Fallback Support**: Falls back to regular bot messages if webhook fails
- **Debug Mode**: Detailed logging for troubleshooting

## Bridge Integration

The webhook spoofing is fully integrated into the main Zefxi bridge. Telegram messages will appear in Discord with:

- **Original Telegram username** (e.g., "@username" or "First Last")
- **Original Telegram profile picture** (automatically fetched)
- **No "Bot" tag** - messages appear as if sent by real users

### Environment Variables

These environment variables are required for the bridge:

```bash
# Required for basic bridge functionality
TELEGRAM_API_ID=your_api_id
TELEGRAM_API_HASH=your_api_hash
TELEGRAM_CHAT_ID=your_chat_id
DISCORD_TOKEN=your_bot_token
DISCORD_SERVER=your_server_id
DISCORD_CHANNEL=your_channel_id

# Required: Discord webhook URL for user spoofing
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/YOUR_WEBHOOK_ID/YOUR_WEBHOOK_TOKEN

# Optional: Enable debug mode
DEBUG=true
```

## Setup

### 1. Create a Webhook (Required)

You must create a webhook in your Discord server:
1. Go to your Discord server settings
2. Navigate to "Integrations" → "Webhooks"
3. Click "Create Webhook"
4. Copy the webhook URL (it should look like: `https://discord.com/api/webhooks/{id}/{token}`)
5. Set the `DISCORD_WEBHOOK_URL` environment variable

### 2. Run the Bridge

```bash
# Set the webhook URL environment variable (required)
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/YOUR_WEBHOOK_ID/YOUR_WEBHOOK_TOKEN"

# Run the bridge
nix develop --command zig build run
```

## How It Works

1. **Telegram Message Received**: User sends message in Telegram
2. **User Info Extraction**: Bridge automatically requests user info from Telegram API
3. **Avatar Fetching**: Bridge fetches user's profile photo URL
4. **Webhook Spoofing**: Message sent via Discord webhook with:
   - Username set to Telegram display name
   - Avatar URL set to Telegram profile picture
   - Original message content
5. **Fallback**: If webhook fails, falls back to regular bot message format

## User Information Caching

The bridge intelligently caches Telegram user information:

- **First Name & Last Name**: Used to create display names
- **Username**: Preferred for display (e.g., "@username")
- **Profile Photos**: Automatically converted to Discord-compatible URLs
- **Smart Caching**: User info cached to avoid repeated API calls

## Message Flow Examples

### Normal Operation (Webhook Spoofing):
```
Telegram: "Hello from Telegram!" (sent by @alice)
Discord:  [Alice's avatar] Alice: "Hello from Telegram!"
```

### Fallback Mode (if webhook fails):
```
Telegram: "Hello from Telegram!" (sent by @alice)
Discord:  🤖 **Alice**: Hello from Telegram!
```

## API Reference

### WebhookExecutor

#### `init(allocator, webhook_url, debug_mode)`
Creates a new webhook executor.

**Parameters:**
- `allocator`: Memory allocator
- `webhook_url`: Discord webhook URL
- `debug_mode`: Enable debug logging

#### `sendSpoofedMessage(content, username, avatar_url)`
Sends a simple text message with optional username and avatar spoofing.

**Parameters:**
- `content`: Message content (required)
- `username`: Custom username (optional, null for default)
- `avatar_url`: Custom avatar URL (optional, null for default)

#### `executeWebhook(payload)`
Sends a complex webhook payload with full Discord API support.

**Parameters:**
- `payload`: WebhookExecutePayload struct

### WebhookExecutePayload

```zig
pub const WebhookExecutePayload = struct {
    content: ?[]const u8 = null,           // Message content
    username: ?[]const u8 = null,          // Custom username
    avatar_url: ?[]const u8 = null,        // Custom avatar URL
    tts: ?bool = null,                     // Text-to-speech
    embeds: ?[]Discord.Embed = null,       // Rich embeds
    allowed_mentions: ?Discord.AllowedMentions = null,
    components: ?[]Discord.MessageComponent = null,
    files: ?[]Discord.FileData = null,     // File attachments
    thread_id: ?Discord.Snowflake = null,  // Send to thread
};
```

### Telegram UserInfo

```zig
pub const UserInfo = struct {
    user_id: i64,
    first_name: []const u8,
    last_name: ?[]const u8,
    username: ?[]const u8,
    avatar_url: ?[]const u8,
    
    pub fn getDisplayName(self: UserInfo, allocator: Allocator) ![]u8;
};
```

## Advanced Examples

### Basic Usage

```bash
# Start bridge (webhook URL is required)
export DISCORD_WEBHOOK_URL="your_webhook_url"
nix develop --command zig build run
```

### Custom Integration

```zig
const webhook = discord.WebhookExecutor.init(allocator, webhook_url, true);

// Spoof a message as a specific user
try webhook.sendSpoofedMessage(
    "Hello from spoofed user!",
    "Custom Username",
    "https://example.com/avatar.png"
);
```

## Debug Mode

Enable debug mode to see detailed information:

```bash
export DEBUG=true
nix develop --command zig build run
```

Debug output includes:
- Webhook HTTP requests and responses
- User info caching operations
- Telegram API requests
- Fallback decisions

## Troubleshooting

### Common Issues

1. **Missing Webhook URL**: The bridge will not start without `DISCORD_WEBHOOK_URL`
2. **Webhook URL Invalid**: Check that the URL is correctly formatted
3. **Missing Avatars**: Telegram users may not have profile photos
4. **Rate Limiting**: Discord webhooks are limited to 5 requests per 2 seconds

### Error Messages

- `Please set DISCORD_WEBHOOK_URL environment variable`: Webhook URL is required
- `WebhookExecutionFailed`: Check webhook URL and Discord permissions
- `User info not cached`: Normal - bridge will request info automatically
- `Webhook failed, falling back`: Webhook issue, but message will still be sent

## Limitations

1. **Rate Limits**: Webhooks are subject to Discord's rate limits (5 requests per 2 seconds per webhook)
2. **Permissions**: Webhooks can only send to the channel they're configured for
3. **Username Length**: Usernames must be 1-80 characters
4. **Avatar URLs**: Must be valid image URLs (Discord will cache them)
5. **Telegram API**: Profile photo URLs may not always be available

## Security Considerations

- Never expose webhook URLs in public repositories
- Consider implementing permission checks before allowing spoofing
- Be aware that webhook messages don't have the same audit trail as bot messages
- Users may be confused by spoofed messages - the bridge clearly indicates message source

## Best Practices

1. **Use Descriptive Setup**: Make it clear when messages are bridged from Telegram
2. **Monitor Rate Limits**: Implement backoff strategies for high-volume usage
3. **Handle Failures Gracefully**: The bridge automatically falls back to regular messages
4. **Cache Management**: User info is automatically cached and cleaned up
5. **Debug Mode**: Use debug mode during setup to troubleshoot issues

## Building and Running

This project uses Nix for dependency management. Make sure you have Nix installed, then use the development shell:

```bash
# Enter the development environment
nix develop

# Build the project
zig build

# Run the bridge (webhook URL required)
zig build run
```

Alternatively, you can run commands directly:
```bash
nix develop --command zig build run
```

## Required Configuration

Before running, you must set up all environment variables:

**Required environment variables:**
- `TELEGRAM_API_ID` - Your Telegram API ID
- `TELEGRAM_API_HASH` - Your Telegram API Hash  
- `TELEGRAM_CHAT_ID` - Target Telegram chat ID
- `DISCORD_TOKEN` - Your Discord bot token
- `DISCORD_SERVER` - Your Discord server ID
- `DISCORD_CHANNEL` - Your Discord channel ID
- `DISCORD_WEBHOOK_URL` - **Required** Discord webhook URL for user spoofing

**Optional environment variables:**
- `DEBUG=true` - Enable detailed logging

The bridge requires the webhook URL to function - user spoofing is the core functionality. 