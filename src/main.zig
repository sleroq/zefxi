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
        
        const webhook_exec = discord.WebhookExecutor.init(
            allocator, 
            config.discord_webhook_url, 
            config.debug_mode
        );
        
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
        
        // Debug user info before getting display name
        print("[Bridge] Processing message from user {d}\n", .{user_info.user_id});
        print("[Bridge] User first_name: '{s}'\n", .{user_info.first_name});
        if (user_info.last_name) |lname| {
            print("[Bridge] User last_name: '{s}'\n", .{lname});
        }
        
        const display_name = user_info.getDisplayName(self.allocator) catch |err| {
            print("[Bridge] Failed to get display name for user {d}: {}\n", .{ user_info.user_id, err });
            return;
        };
        defer self.allocator.free(display_name);
        
        print("[Bridge] Generated display_name: '{s}'\n", .{display_name});
        
        if (self.config.debug_mode) {
            print("[Bridge] Processing message from user {s} ({d})\n", .{ display_name, user_info.user_id });
        }
        
        switch (content) {
            .text => |message_text| {
                self.handleTextMessage(chat_id, display_name, user_info, message_text);
            },
            .attachment => |attachment_info| {
                self.handleAttachmentMessage(chat_id, display_name, user_info, attachment_info);
            },
            .text_with_attachment => |text_attachment| {
                self.handleTextWithAttachmentMessage(chat_id, display_name, user_info, text_attachment.text, text_attachment.attachment);
            },
        }
    }

    fn handleTextMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, message_text: []const u8) void {
        // Validate strings before printing to avoid crashes
        const safe_display_name = if (std.unicode.utf8ValidateSlice(display_name)) display_name else "<invalid_utf8_name>";
        const safe_message_text = if (std.unicode.utf8ValidateSlice(message_text)) message_text else "<invalid_utf8_message>";
        
        print("[Bridge] Telegram -> Discord: Chat {d}, User {s}: {s}\n", .{ chat_id, safe_display_name, safe_message_text });
        print("[Bridge] User avatar_url: {?s}\n", .{user_info.avatar_url});
        
        if (message_text.len == 0) return;
        
        const escaped_message = self.escapeDiscordMarkdown(message_text) catch message_text;
        defer if (escaped_message.ptr != message_text.ptr) self.allocator.free(escaped_message);
        
        self.webhook_executor.sendSpoofedMessage(
            escaped_message,
            display_name,
            user_info.avatar_url
        ) catch |err| {
            print("[Bridge] Webhook failed: {}, falling back to regular message\n", .{err});
            self.sendRegularDiscordMessage(escaped_message, display_name);
        };
    }

    fn handleAttachmentMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, attachment_info: telegram.AttachmentInfo) void {
        print("[Bridge] Telegram -> Discord: Chat {d}, User {s}: [attachment]\n", .{ chat_id, display_name });
        
        if (attachment_info.url) |attachment_url| {
            if (self.config.debug_mode) {
                print("[Bridge] Sending attachment to Discord: {s}\n", .{attachment_url});
            }
            
            switch (attachment_info.attachment_type) {
                .photo => {
                    self.webhook_executor.sendSpoofedMessageWithImage(
                        null,
                        display_name,
                        user_info.avatar_url,
                        attachment_url
                    ) catch |err| {
                        print("[Bridge] Photo webhook failed: {}, falling back\n", .{err});
                        self.sendRegularDiscordMessage(attachment_url, display_name);
                    };
                },
                .sticker, .animation => {
                    self.webhook_executor.sendSpoofedMessageWithAnimation(
                        null,
                        display_name,
                        user_info.avatar_url,
                        attachment_url
                    ) catch |err| {
                        print("[Bridge] Animation webhook failed: {}, falling back\n", .{err});
                        self.sendRegularDiscordMessage(attachment_url, display_name);
                    };
                },
                else => {
                    self.webhook_executor.sendSpoofedMessage(
                        attachment_url,
                        display_name,
                        user_info.avatar_url
                    ) catch |err| {
                        print("[Bridge] Attachment webhook failed: {}, falling back\n", .{err});
                        self.sendRegularDiscordMessage(attachment_url, display_name);
                    };
                },
            }
        } else {
            print("[Bridge] Attachment still downloading, will send when ready\n", .{});
        }
    }

    fn handleTextWithAttachmentMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, text: []const u8, attachment: telegram.AttachmentInfo) void {
        print("[Bridge] Telegram -> Discord: Chat {d}, User {s}: {s} [attachment]\n", .{ chat_id, display_name, text });
        
        const escaped_caption = self.escapeDiscordMarkdown(text) catch text;
        defer if (escaped_caption.ptr != text.ptr) self.allocator.free(escaped_caption);
        
        if (attachment.url) |attachment_url| {
            switch (attachment.attachment_type) {
                .photo => {
                    self.webhook_executor.sendSpoofedMessageWithImage(
                        escaped_caption,
                        display_name,
                        user_info.avatar_url,
                        attachment_url
                    ) catch |err| {
                        print("[Bridge] Photo with caption webhook failed: {}\n", .{err});
                        self.sendFallbackMessage(escaped_caption, attachment_url, display_name);
                    };
                },
                .sticker, .animation => {
                    self.webhook_executor.sendSpoofedMessageWithAnimation(
                        escaped_caption,
                        display_name,
                        user_info.avatar_url,
                        attachment_url
                    ) catch |err| {
                        print("[Bridge] Animation with caption webhook failed: {}\n", .{err});
                        self.sendFallbackMessage(escaped_caption, attachment_url, display_name);
                    };
                },
                else => {
                    const message_with_link = std.fmt.allocPrint(
                        self.allocator,
                        "{s}\n{s}",
                        .{ escaped_caption, attachment_url }
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
                        print("[Bridge] Attachment with caption webhook failed: {}\n", .{err});
                        self.sendRegularDiscordMessage(message_with_link, display_name);
                    };
                },
            }
        } else {
            print("[Bridge] Attachment with caption still downloading, will send when ready\n", .{});
        }
    }

    fn sendFallbackMessage(self: *Self, caption: []const u8, attachment_url: []const u8, display_name: []const u8) void {
        const fallback_message = std.fmt.allocPrint(
            self.allocator,
            "{s}\n{s}",
            .{ caption, attachment_url }
        ) catch {
            print("[Bridge] Failed to format fallback message with caption\n", .{});
            return;
        };
        defer self.allocator.free(fallback_message);
        
        self.sendRegularDiscordMessage(fallback_message, display_name);
    }

    fn sendRegularDiscordMessage(self: *Self, message_text: []const u8, sender_name: []const u8) void {
        const formatted_message = std.fmt.allocPrint(
            self.allocator,
            "**{s}**: {s}",
            .{ sender_name, message_text }
        ) catch {
            print("[Bridge] Failed to format message\n", .{});
            return;
        };
        defer self.allocator.free(formatted_message);
        
        var result = self.discord_client.session.api.sendMessage(self.config.discord_channel_id, .{
            .content = formatted_message,
        }) catch |err| {
            print("[Bridge] Failed to send message to Discord: {}\n", .{err});
            return;
        };
        defer result.deinit();

        if (self.config.debug_mode) {
            const m = result.value.unwrap();
            print("[Bridge] Sent to Discord: {?s}\n", .{m.content});
        }
    }

    fn onDiscordMessage(ctx: *anyopaque, channel_id: []const u8, _: []const u8, username: []const u8, message_text: []const u8, attachments: ?[]const Discord.Attachment) void {
        const self: *Self = @ptrCast(@alignCast(ctx));
        
        // Skip messages that appear to be from bridge fallback
        if (std.mem.startsWith(u8, message_text, "**")) {
            if (self.config.debug_mode) {
                print("[Bridge] Skipping bridge fallback message\n", .{});
            }
            return;
        }
        
        const has_text = message_text.len > 0;
        const has_attachments = attachments != null and attachments.?.len > 0;
        
        if (has_attachments) {
            print("[Bridge] Discord -> Telegram: Channel {s}, User {s}: {s} [with {} attachment(s)]\n", .{ 
                channel_id, username, message_text, attachments.?.len 
            });
        } else {
            print("[Bridge] Discord -> Telegram: Channel {s}, User {s}: {s}\n", .{ 
                channel_id, username, message_text 
            });
        }
        
        if (!has_text and !has_attachments) {
            if (self.config.debug_mode) {
                print("[Bridge] Skipping empty message\n", .{});
            }
            return;
        }
        
        if (self.config.telegram_chat_id) |chat_id| {
            if (has_text and has_attachments) {
                // Message with both text and attachments - send as one combined message
                var message_parts = std.ArrayList([]const u8).init(self.allocator);
                defer message_parts.deinit();
                
                // Add the text content
                const text_part = std.fmt.allocPrint(
                    self.allocator,
                    "{s}: {s}",
                    .{ username, message_text }
                ) catch {
                    print("[Bridge] Failed to format text part for Telegram\n", .{});
                    return;
                };
                defer self.allocator.free(text_part);
                message_parts.append(text_part) catch return;
                
                // Add attachment info
                for (attachments.?) |attachment| {
                    const attachment_part = std.fmt.allocPrint(
                        self.allocator,
                        "[Attachment: {s}] {s}",
                        .{ attachment.filename, attachment.url }
                    ) catch {
                        print("[Bridge] Failed to format attachment part for Telegram\n", .{});
                        continue;
                    };
                    defer self.allocator.free(attachment_part);
                    message_parts.append(attachment_part) catch continue;
                }
                
                // Combine all parts
                const combined_message = std.mem.join(self.allocator, "\n", message_parts.items) catch {
                    print("[Bridge] Failed to join message parts for Telegram\n", .{});
                    return;
                };
                defer self.allocator.free(combined_message);
                
                self.telegram_client.sendMessage(chat_id, combined_message) catch |err| {
                    print("[Bridge] Failed to send combined message to Telegram: {}\n", .{err});
                };
                
            } else if (has_text) {
                // Text-only message
                const formatted_message = std.fmt.allocPrint(
                    self.allocator, 
                    "{s}: {s}", 
                    .{ username, message_text }
                ) catch {
                    print("[Bridge] Failed to format message for Telegram\n", .{});
                    return;
                };
                defer self.allocator.free(formatted_message);
                
                self.telegram_client.sendMessage(chat_id, formatted_message) catch |err| {
                    print("[Bridge] Failed to send message to Telegram: {}\n", .{err});
                };
                
            } else if (has_attachments) {
                // Attachment-only message
                for (attachments.?) |attachment| {
                    if (self.config.debug_mode) {
                        print("[Bridge] Processing Discord attachment: {s} ({})\n", .{ attachment.filename, attachment.size });
                    }
                    
                    const attachment_message = std.fmt.allocPrint(
                        self.allocator,
                        "{s}: [Attachment: {s}]\n{s}",
                        .{ username, attachment.filename, attachment.url }
                    ) catch {
                        print("[Bridge] Failed to format attachment message for Telegram\n", .{});
                        continue;
                    };
                    defer self.allocator.free(attachment_message);
                    
                    self.telegram_client.sendMessage(chat_id, attachment_message) catch |err| {
                        print("[Bridge] Failed to send attachment message to Telegram: {}\n", .{err});
                    };
                }
            }
        } else {
            print("[Bridge] No Telegram chat configured, message not forwarded\n", .{});
        }
    }

    pub fn start(self: *Self) !void {
        print("\n=== Starting Zefxi Bridge ===\n", .{});
        print("Telegram Chat ID: {?d}\n", .{self.config.telegram_chat_id});
        print("Discord Channel ID: {s}\n", .{self.config.discord_channel_id});
        print("File Server Port: {d}\n", .{self.config.avatar_server_port});
        print("Debug mode: {}\n", .{self.config.debug_mode});
        print("Press Ctrl+C to exit\n\n", .{});

        // Set up handlers and connections
        self.telegram_client.setMessageHandler(self, &onTelegramMessage);
        self.discord_client.setMessageHandler(self, &onDiscordMessage);
        self.discord_client.setWebhookExecutor(self.webhook_executor);
        self.avatar_server.setFileIdMapping(&self.telegram_client.file_id_to_path);
        
        // Start services
        print("[Bridge] Starting file HTTP server...\n", .{});
        const avatar_server_thread = try std.Thread.spawn(.{}, startAvatarServer, .{&self.avatar_server});
        defer avatar_server_thread.join();

        print("[Bridge] Starting Telegram client...\n", .{});
        try self.telegram_client.start();

        print("[Bridge] Starting Discord client...\n", .{});
        const discord_thread = try std.Thread.spawn(.{}, startDiscordClient, .{&self.discord_client});
        defer discord_thread.join();

        print("[Bridge] Bridge is now running!\n", .{});
        while (true) {
            if (!try self.telegram_client.tick()) {
                break;
            }
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

fn getRequiredEnvVar(allocator: Allocator, env_var: []const u8, description: []const u8) ![]const u8 {
    return std.process.getEnvVarOwned(allocator, env_var) catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set {s} environment variable\n", .{env_var});
            print("{s}\n", .{description});
            return err;
        },
        else => return err,
    };
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // Get required environment variables
    const api_id_str = try getRequiredEnvVar(allocator, "TELEGRAM_API_ID", "Get your API credentials from https://my.telegram.org/apps");
    defer allocator.free(api_id_str);

    const api_hash = try getRequiredEnvVar(allocator, "TELEGRAM_API_HASH", "Get your API credentials from https://my.telegram.org/apps");
    defer allocator.free(api_hash);

    const discord_token = try getRequiredEnvVar(allocator, "DISCORD_TOKEN", "Get your bot token from https://discord.com/developers/applications");
    defer allocator.free(discord_token);

    const discord_channel_id = try getRequiredEnvVar(allocator, "DISCORD_CHANNEL", "This should be the Discord channel ID where messages will be bridged");
    defer allocator.free(discord_channel_id);

    const discord_server_id = try getRequiredEnvVar(allocator, "DISCORD_SERVER", "This should be the Discord server ID where messages will be bridged");
    defer allocator.free(discord_server_id);

    const discord_webhook_url = try getRequiredEnvVar(allocator, "DISCORD_WEBHOOK_URL", "Create a webhook in your Discord channel settings");
    defer allocator.free(discord_webhook_url);

    // Parse required values
    const api_id = std.fmt.parseInt(i32, api_id_str, 10) catch {
        print("Error: TELEGRAM_API_ID must be a valid integer\n", .{});
        return;
    };

    const telegram_chat_id = parseEnvI64(allocator, "TELEGRAM_CHAT_ID") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_CHAT_ID environment variable\n", .{});
            return;
        },
        else => return err,
    };

    const discord_server_id_parsed = Discord.Snowflake.fromRaw(discord_server_id) catch {
        print("Error: DISCORD_SERVER must be a valid Discord snowflake\n", .{});
        return;
    };

    const discord_channel_id_parsed = Discord.Snowflake.fromRaw(discord_channel_id) catch {
        print("Error: DISCORD_CHANNEL must be a valid Discord snowflake\n", .{});
        return;
    };

    // Get optional environment variables
    var debug_mode = false;
    if (std.process.getEnvVarOwned(allocator, "DEBUG")) |debug_str| {
        defer allocator.free(debug_str);
        debug_mode = std.mem.eql(u8, debug_str, "1") or std.mem.eql(u8, debug_str, "true");
    } else |_| {}

    const avatar_base_url = std.process.getEnvVarOwned(allocator, "AVATAR_BASE_URL") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => try allocator.dupe(u8, "http://127.0.0.1:8080"),
        else => return err,
    };
    defer allocator.free(avatar_base_url);

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