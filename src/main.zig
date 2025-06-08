const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const telegram = @import("telegram.zig");
const discord = @import("discord.zig");
const http_server = @import("http_server.zig");
const Discord = @import("discord");

const BridgeConfig = struct {
    debug_mode: bool = false,
    telegram_api_id: i32,
    telegram_api_hash: []const u8,
    telegram_chat_id: ?i64,
    discord_token: []const u8,
    discord_server_id: Discord.Snowflake,
    discord_channel_id: Discord.Snowflake,
    discord_webhook_url: []const u8,
    avatar_server_port: u16 = 8080,
    files_directory: []const u8 = "tdlib",
    avatar_base_url: []const u8 = "http://127.0.0.1:8080",
};

const Bridge = struct {
    allocator: Allocator,
    config: BridgeConfig,
    telegram_client: telegram.TelegramClient,
    discord_client: discord.DiscordClient,
    webhook_executor: discord.WebhookExecutor,
    avatar_server: http_server.HttpServer,
    
    const Self = @This();

    pub fn init(allocator: Allocator, config: BridgeConfig) !Self {
        const telegram_config = telegram.Config{
            .debug_mode = config.debug_mode,
            .receive_timeout = 0.1,
            .avatar_base_url = config.avatar_base_url,
        };
        
        const tg_client = try telegram.TelegramClient.init(
            allocator,
            config.telegram_api_id,
            config.telegram_api_hash,
            config.telegram_chat_id,
            telegram_config
        );
        
        const dc_client = try discord.DiscordClient.init(
            allocator,
            config.discord_token,
            config.discord_server_id,
            config.discord_channel_id,
            config.debug_mode
        );
        
        // Create webhook executor (now required)
        const webhook_exec = discord.WebhookExecutor.init(
            allocator, 
            config.discord_webhook_url, 
            config.debug_mode
        );
        
        // Create file HTTP server
        const avatar_srv = http_server.HttpServer.init(
            allocator,
            config.avatar_server_port,
            config.files_directory,
            config.debug_mode
        );
        
        return Self{
            .allocator = allocator,
            .config = config,
            .telegram_client = tg_client,
            .discord_client = dc_client,
            .webhook_executor = webhook_exec,
            .avatar_server = avatar_srv,
        };
    }

    pub fn deinit(self: *Self) void {
        self.telegram_client.deinit();
        self.discord_client.deinit();
    }

    fn escapeDiscordMarkdown(self: *Self, text: []const u8) ![]const u8 {
        // Simple escape for Discord markdown characters
        var result = std.ArrayList(u8).init(self.allocator);
        defer result.deinit();
        
        for (text) |char| {
            switch (char) {
                '*', '_', '`', '~', '|', '\\' => {
                    try result.append('\\');
                    try result.append(char);
                },
                else => try result.append(char),
            }
        }
        
        return result.toOwnedSlice();
    }

    fn onTelegramMessage(ctx: *anyopaque, chat_id: i64, user_info: telegram.UserInfo, content: telegram.MessageContent) void {
        const self: *Self = @ptrCast(@alignCast(ctx));
        
        // Get user display name
        const display_name = user_info.getDisplayName(self.allocator) catch {
            print("[Bridge] Failed to get display name for user {d}\n", .{user_info.user_id});
            return;
        };
        defer self.allocator.free(display_name);
        
        // Debug user info
        if (self.config.debug_mode) {
            print("[Bridge] 🔍 User info debug:\n", .{});
            print("[Bridge]   - user_id: {d}\n", .{user_info.user_id});
            print("[Bridge]   - first_name: {s}\n", .{user_info.first_name});
            print("[Bridge]   - last_name: {?s}\n", .{user_info.last_name});
            print("[Bridge]   - username: {?s}\n", .{user_info.username});
            print("[Bridge]   - avatar_url: {?s}\n", .{user_info.avatar_url});
        }
        
        switch (content) {
            .text => |message_text| {
                print("[Bridge] Telegram -> Discord: Chat {d}, User {s} ({d}): {s}\n", .{ 
                    chat_id, display_name, user_info.user_id, message_text 
                });
                
                // Skip empty messages
                if (message_text.len == 0) {
                    return;
                }
                
                // Escape Discord markdown characters in the message
                const escaped_message = self.escapeDiscordMarkdown(message_text) catch message_text;
                defer if (escaped_message.ptr != message_text.ptr) self.allocator.free(escaped_message);
                
                // Use webhook spoofing for text messages
                if (self.config.debug_mode) {
                    print("[Bridge] Using webhook spoofing for text message from user: {s}\n", .{display_name});
                }
                
                self.webhook_executor.sendSpoofedMessage(
                    escaped_message,
                    display_name,
                    user_info.avatar_url
                ) catch |err| {
                    print("[Bridge] Webhook failed: {}, falling back to regular message\n", .{err});
                    // Fallback to regular Discord API
                    self.sendRegularDiscordMessage(escaped_message, display_name);
                };
            },
            .attachment => |attachment_info| {
                const attachment_emoji = attachment_info.getEmoji();
                const attachment_name = attachment_info.getDisplayName();
                
                // Create descriptive message based on attachment type
                var description_parts = std.ArrayList([]const u8).init(self.allocator);
                defer description_parts.deinit();
                
                description_parts.append(attachment_name) catch |err| {
                    print("[Bridge] Failed to append attachment name: {}\n", .{err});
                    return;
                };
                
                if (attachment_info.width) |w| {
                    if (attachment_info.height) |h| {
                        const size_str = std.fmt.allocPrint(self.allocator, " ({d}x{d})", .{ w, h }) catch {
                            print("[Bridge] Failed to format size string\n", .{});
                            return;
                        };
                        defer self.allocator.free(size_str);
                        description_parts.append(size_str) catch |err| {
                            print("[Bridge] Failed to append size string: {}\n", .{err});
                            return;
                        };
                    }
                }
                
                if (attachment_info.duration) |d| {
                    const duration_str = std.fmt.allocPrint(self.allocator, " ({d}s)", .{d}) catch {
                        print("[Bridge] Failed to format duration string\n", .{});
                        return;
                    };
                    defer self.allocator.free(duration_str);
                    description_parts.append(duration_str) catch |err| {
                        print("[Bridge] Failed to append duration string: {}\n", .{err});
                        return;
                    };
                }
                
                if (attachment_info.file_size) |size| {
                    const size_mb = @as(f64, @floatFromInt(size)) / (1024.0 * 1024.0);
                    if (size_mb >= 1.0) {
                        const size_str = std.fmt.allocPrint(self.allocator, " ({d:.1} MB)", .{size_mb}) catch {
                            print("[Bridge] Failed to format size MB string\n", .{});
                            return;
                        };
                        defer self.allocator.free(size_str);
                        description_parts.append(size_str) catch |err| {
                            print("[Bridge] Failed to append size MB string: {}\n", .{err});
                            return;
                        };
                    } else {
                        const size_kb = @as(f64, @floatFromInt(size)) / 1024.0;
                        const size_str = std.fmt.allocPrint(self.allocator, " ({d:.1} KB)", .{size_kb}) catch {
                            print("[Bridge] Failed to format size KB string\n", .{});
                            return;
                        };
                        defer self.allocator.free(size_str);
                        description_parts.append(size_str) catch |err| {
                            print("[Bridge] Failed to append size KB string: {}\n", .{err});
                            return;
                        };
                    }
                }
                
                const description = std.mem.join(self.allocator, "", description_parts.items) catch {
                    print("[Bridge] Failed to join description parts\n", .{});
                    return;
                };
                defer self.allocator.free(description);
                
                print("[Bridge] Telegram -> Discord: Chat {d}, User {s} ({d}): {s} {s}\n", .{ 
                    chat_id, display_name, user_info.user_id, attachment_emoji, description
                });
                
                if (attachment_info.url) |attachment_url| {
                    // Attachment is ready, send it to Discord
                    if (self.config.debug_mode) {
                        print("[Bridge] 📁 Sending attachment to Discord: {s}\n", .{attachment_url});
                    }
                    
                    // For photos, stickers, and animations, use image embed
                    if (attachment_info.attachment_type == .photo or 
                        attachment_info.attachment_type == .sticker or 
                        attachment_info.attachment_type == .animation) {
                        self.webhook_executor.sendSpoofedMessageWithImage(
                            null, // No caption
                            display_name,
                            user_info.avatar_url,
                            attachment_url
                        ) catch |err| {
                            print("[Bridge] Attachment webhook failed: {}, falling back to text message\n", .{err});
                            const fallback_message = std.fmt.allocPrint(
                                self.allocator,
                                "{s} {s} - {s}",
                                .{ attachment_emoji, description, attachment_url }
                            ) catch {
                                print("[Bridge] Failed to format fallback message\n", .{});
                                return;
                            };
                            defer self.allocator.free(fallback_message);
                            
                            self.sendRegularDiscordMessage(fallback_message, display_name);
                        };
                    } else {
                        // For other file types, send as text with link
                        const message_with_link = std.fmt.allocPrint(
                            self.allocator,
                            "{s} {s}\n{s}",
                            .{ attachment_emoji, description, attachment_url }
                        ) catch {
                            print("[Bridge] Failed to format message with link\n", .{});
                            return;
                        };
                        defer self.allocator.free(message_with_link);
                        
                        self.webhook_executor.sendSpoofedMessage(
                            message_with_link,
                            display_name,
                            user_info.avatar_url
                        ) catch |err| {
                            print("[Bridge] Attachment webhook failed: {}, falling back to regular message\n", .{err});
                            self.sendRegularDiscordMessage(message_with_link, display_name);
                        };
                    }
                } else {
                    // Attachment is still downloading, wait for it to be ready
                    print("[Bridge] ⏳ {s} still downloading, will send when ready\n", .{attachment_name});
                }
            },
            .text_with_attachment => |text_attachment| {
                const attachment_emoji = text_attachment.attachment.getEmoji();
                const attachment_name = text_attachment.attachment.getDisplayName();
                
                print("[Bridge] Telegram -> Discord: Chat {d}, User {s} ({d}): {s} [{s} {s}]\n", .{ 
                    chat_id, display_name, user_info.user_id, text_attachment.text, attachment_emoji, attachment_name
                });
                
                // Escape Discord markdown characters in the caption
                const escaped_caption = self.escapeDiscordMarkdown(text_attachment.text) catch text_attachment.text;
                defer if (escaped_caption.ptr != text_attachment.text.ptr) self.allocator.free(escaped_caption);
                
                if (text_attachment.attachment.url) |attachment_url| {
                    // Attachment is ready, send it to Discord with caption
                    if (self.config.debug_mode) {
                        print("[Bridge] 📁 Sending attachment with caption to Discord: {s}\n", .{attachment_url});
                    }
                    
                    // For photos, stickers, and animations, use image embed with caption
                    if (text_attachment.attachment.attachment_type == .photo or 
                        text_attachment.attachment.attachment_type == .sticker or 
                        text_attachment.attachment.attachment_type == .animation) {
                        self.webhook_executor.sendSpoofedMessageWithImage(
                            escaped_caption,
                            display_name,
                            user_info.avatar_url,
                            attachment_url
                        ) catch |err| {
                            print("[Bridge] Attachment with caption webhook failed: {}, falling back to text message\n", .{err});
                            const fallback_message = std.fmt.allocPrint(
                                self.allocator,
                                "{s}\n{s} {s} - {s}",
                                .{ escaped_caption, attachment_emoji, attachment_name, attachment_url }
                            ) catch {
                                print("[Bridge] Failed to format fallback message with caption\n", .{});
                                return;
                            };
                            defer self.allocator.free(fallback_message);
                            
                            self.sendRegularDiscordMessage(fallback_message, display_name);
                        };
                    } else {
                        // For other file types, send caption and link
                        const message_with_link = std.fmt.allocPrint(
                            self.allocator,
                            "{s}\n{s} {s}\n{s}",
                            .{ escaped_caption, attachment_emoji, attachment_name, attachment_url }
                        ) catch {
                            print("[Bridge] Failed to format message with link and caption\n", .{});
                            return;
                        };
                        defer self.allocator.free(message_with_link);
                        
                        self.webhook_executor.sendSpoofedMessage(
                            message_with_link,
                            display_name,
                            user_info.avatar_url
                        ) catch |err| {
                            print("[Bridge] Attachment with caption webhook failed: {}, falling back to regular message\n", .{err});
                            self.sendRegularDiscordMessage(message_with_link, display_name);
                        };
                    }
                } else {
                    // Attachment is still downloading, wait for it to be ready
                    print("[Bridge] ⏳ {s} with caption still downloading, will send when ready\n", .{attachment_name});
                }
            },
        }
    }

    fn sendRegularDiscordMessage(self: *Self, message_text: []const u8, sender_name: []const u8) void {
        const channel_id = self.config.discord_channel_id;
        
        // Format message with sender name
        const formatted_message = std.fmt.allocPrint(
            self.allocator,
            "**{s}**: {s}",
            .{ sender_name, message_text }
        ) catch {
            print("[Bridge] Failed to format message\n", .{});
            return;
        };
        defer self.allocator.free(formatted_message);
        
        var result = self.discord_client.session.api.sendMessage(channel_id, .{
            .content = formatted_message,
        }) catch |err| {
            print("[Bridge] Failed to send message to Discord: {}\n", .{err});
            return;
        };

        defer result.deinit();

        const m = result.value.unwrap();
        if (self.config.debug_mode) {
            std.debug.print("[Bridge] Sent to Discord: {?s}\n", .{m.content});
        }
    }

    fn onDiscordMessage(ctx: *anyopaque, channel_id: []const u8, user_id: []const u8, username: []const u8, message_text: []const u8) void {
        const self: *Self = @ptrCast(@alignCast(ctx));
        print("[Bridge] Discord -> Telegram: Channel {s}, User {s} ({s}): {s}\n", .{ channel_id, username, user_id, message_text });
        
        // Skip empty messages
        if (message_text.len == 0) {
            return;
        }
        
        // Skip messages that look like they came from our webhook (to avoid loops)
        // Check if message starts with ** (markdown bold) which indicates it's from our regular fallback
        if (std.mem.startsWith(u8, message_text, "**")) {
            if (self.config.debug_mode) {
                print("[Bridge] Skipping message that appears to be from bridge fallback\n", .{});
            }
            return;
        }
        
        // Forward to Telegram if we have a target chat
        if (self.config.telegram_chat_id) |chat_id| {
            const formatted_message = std.fmt.allocPrint(
                self.allocator, 
                "{s}: {s}", 
                .{ username, message_text }
            ) catch {
                print("[Bridge] Failed to format message for Telegram\n", .{});
                return;
            };
            defer self.allocator.free(formatted_message);
            
            // Handle error to return void
            self.telegram_client.sendMessage(chat_id, formatted_message) catch |err| {
                print("[Bridge] Failed to send message to Telegram: {}\n", .{err});
                return;
            };
        } else {
            print("[Bridge] No Telegram chat configured, message not forwarded\n", .{});
        }
    }

    pub fn start(self: *Self) !void {
        print("\n=== Starting Zefxi Bridge with User Spoofing ===\n", .{});
        print("Telegram Chat ID: {?d}\n", .{self.config.telegram_chat_id});
        print("Discord Channel ID: {s}\n", .{self.config.discord_channel_id});
        print("Webhook Spoofing: Enabled\n", .{});
        print("File Server Port: {d}\n", .{self.config.avatar_server_port});
        print("Files Directory: {s}\n", .{self.config.files_directory});
        print("File Base URL: {s}\n", .{self.config.avatar_base_url});
        print("Debug mode: {}\n", .{self.config.debug_mode});
        print("Press Ctrl+C to exit\n\n", .{});

        // Set up message handlers
        self.telegram_client.setMessageHandler(self, &onTelegramMessage);
        self.discord_client.setMessageHandler(self, &onDiscordMessage);
        
        // Set up webhook executor in Discord client
        self.discord_client.setWebhookExecutor(self.webhook_executor);

        // Set up file ID mapping for HTTP server
        self.avatar_server.setFileIdMapping(&self.telegram_client.file_id_to_path);
        
        // Start file HTTP server in a separate thread
        print("[Bridge] Starting file HTTP server...\n", .{});
        const avatar_server_thread = try std.Thread.spawn(.{}, startAvatarServer, .{&self.avatar_server});
        defer avatar_server_thread.join();

        // Start Telegram client
        print("[Bridge] Starting Telegram client...\n", .{});
        try self.telegram_client.start();

        // Start Discord client in a separate thread
        print("[Bridge] Starting Discord client...\n", .{});
        const discord_thread = try std.Thread.spawn(.{}, startDiscordClient, .{&self.discord_client});
        defer discord_thread.join();

        // Main event loop for Telegram
        print("[Bridge] Bridge is now running with user spoofing and avatar server!\n", .{});
        while (true) {
            if (!try self.telegram_client.tick()) {
                break;
            }
            
            // Small delay to prevent busy waiting
            std.time.sleep(10 * std.time.ns_per_ms);
        }
    }

    fn startDiscordClient(client: *discord.DiscordClient) void {
        client.start() catch |err| {
            print("[Bridge] Discord client error: {}\n", .{err});
        };
    }

    fn startAvatarServer(server: *http_server.HttpServer) void {
        server.start() catch |err| {
            print("[Bridge] File server error: {}\n", .{err});
        };
    }
};

fn parseEnvI64(allocator: Allocator, env_var: []const u8) !?i64 {
    const value = std.process.getEnvVarOwned(allocator, env_var) catch |err| switch (err) {
        error.EnvironmentVariableNotFound => return null,
        else => return err,
    };
    defer allocator.free(value);

    return std.fmt.parseInt(i64, value, 10) catch |err| {
        print("Warning: Invalid {s} format: {}\n", .{ env_var, err });
        return err;
    };
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // Get Telegram API credentials
    const api_id_str = std.process.getEnvVarOwned(allocator, "TELEGRAM_API_ID") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_API_ID environment variable\n", .{});
            print("Get your API credentials from https://my.telegram.org/apps\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(api_id_str);

    const api_hash = std.process.getEnvVarOwned(allocator, "TELEGRAM_API_HASH") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_API_HASH environment variable\n", .{});
            print("Get your API credentials from https://my.telegram.org/apps\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(api_hash);

    const api_id = std.fmt.parseInt(i32, api_id_str, 10) catch {
        print("Error: TELEGRAM_API_ID must be a valid integer\n", .{});
        return;
    };

    // Get Discord token
    const discord_token = std.process.getEnvVarOwned(allocator, "DISCORD_TOKEN") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set DISCORD_TOKEN environment variable\n", .{});
            print("Get your bot token from https://discord.com/developers/applications\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(discord_token);

    // Get Discord channel
    const discord_channel_id = std.process.getEnvVarOwned(allocator, "DISCORD_CHANNEL") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set DISCORD_CHANNEL environment variable\n", .{});
            print("This should be the Discord channel ID where messages will be bridged\n", .{});
            print("To get a channel ID:\n", .{});
            print("  1. Enable Developer Mode in Discord settings\n", .{});
            print("  2. Right-click on the target channel\n", .{});
            print("  3. Select 'Copy Channel ID'\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(discord_channel_id);

    const telegram_chat_id = parseEnvI64(allocator, "TELEGRAM_CHAT_ID") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_CHAT_ID environment variable\n", .{});
            print("This should be the Telegram chat ID where messages will be bridged\n", .{});
            return;
        },
        else => return err,
    };

    const discord_server_id = std.process.getEnvVarOwned(allocator, "DISCORD_SERVER") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set DISCORD_SERVER environment variable\n", .{});
            print("This should be the Discord server ID where messages will be bridged\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(discord_server_id);

    // Get required Discord webhook URL for user spoofing
    const discord_webhook_url = std.process.getEnvVarOwned(allocator, "DISCORD_WEBHOOK_URL") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set DISCORD_WEBHOOK_URL environment variable\n", .{});
            print("This is required for user spoofing functionality.\n", .{});
            print("To create a webhook:\n", .{});
            print("  1. Go to your Discord channel settings\n", .{});
            print("  2. Navigate to 'Integrations' → 'Webhooks'\n", .{});
            print("  3. Click 'Create Webhook'\n", .{});
            print("  4. Copy the webhook URL\n", .{});
            print("  5. Set DISCORD_WEBHOOK_URL environment variable\n", .{});
            print("Example: export DISCORD_WEBHOOK_URL=\"https://discord.com/api/webhooks/ID/TOKEN\"\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(discord_webhook_url);

    // Get debug mode
    var debug_mode = false;
    if (std.process.getEnvVarOwned(allocator, "DEBUG")) |debug_str| {
        defer allocator.free(debug_str);
        debug_mode = std.mem.eql(u8, debug_str, "1") or std.mem.eql(u8, debug_str, "true");
    } else |_| {
        // Default to false
    }

    // Get avatar base URL (optional, defaults to localhost)
    const avatar_base_url = std.process.getEnvVarOwned(allocator, "AVATAR_BASE_URL") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => try allocator.dupe(u8, "http://127.0.0.1:8080"),
        else => return err,
    };
    defer allocator.free(avatar_base_url);

    const discord_server_id_parsed = Discord.Snowflake.fromRaw(discord_server_id) catch {
        print("Error: DISCORD_SERVER must be a valid Discord snowflake\n", .{});
        return;
    };

    const discord_channel_id_parsed = Discord.Snowflake.fromRaw(discord_channel_id) catch {
        print("Error: DISCORD_CHANNEL must be a valid Discord snowflake\n", .{});
        return;
    };

    const config = BridgeConfig{
        .debug_mode = debug_mode,
        .telegram_api_id = api_id,
        .telegram_api_hash = api_hash,
        .telegram_chat_id = telegram_chat_id,
        .discord_token = discord_token,
        .discord_server_id = discord_server_id_parsed,
        .discord_channel_id = discord_channel_id_parsed,
        .discord_webhook_url = discord_webhook_url,
        .avatar_base_url = avatar_base_url,
    };

    var bridge = Bridge.init(allocator, config) catch |err| {
        print("Failed to initialize bridge: {}\n", .{err});
        return;
    };
    defer bridge.deinit();

    try bridge.start();

    print("\nBridge session completed!\n", .{});
} 