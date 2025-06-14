const std = @import("std");
const log = std.log.scoped(.bridge);
const Allocator = std.mem.Allocator;
const telegram = @import("telegram/telegram.zig");
const discord = @import("discord.zig");
const Config = @import("config.zig");
const Discord = @import("discord");
const helpers = @import("helpers.zig");
const telegram_event = @import("telegram/telegram_event.zig");

const BridgeConfig = Config.BridgeConfig;

const Bridge = struct {
    allocator: Allocator,
    config: BridgeConfig,
    telegram_client: telegram.TelegramClient,
    discord_client: discord.DiscordClient,
    dc_webhook_executor: discord.WebhookExecutor,

    const Self = @This();

    pub fn init(allocator: Allocator, config: BridgeConfig) !Self {
        const telegram_config = telegram.Config{
            .debug_mode = config.debug_mode,
            .receive_timeout = 0.1,
        };

        const tg_client = try telegram.TelegramClient.init(
            allocator,
            config.telegram_api_id,
            config.telegram_api_hash,
            config.telegram_chat_id,
            telegram_config,
        );

        const dc_client = try discord.DiscordClient.init(
            allocator,
            config.discord_token,
            config.discord_server_id,
            config.discord_channel_id,
            config.debug_mode,
        );

        const dc_webhook_exec = discord.WebhookExecutor.init(
            allocator,
            config.discord_webhook_url,
            config.debug_mode,
        );

        return Self{
            .allocator = allocator,
            .config = config,
            .telegram_client = tg_client,
            .discord_client = dc_client,
            .dc_webhook_executor = dc_webhook_exec,
        };
    }

    pub fn deinit(self: *Self) void {
        self.telegram_client.deinit();
        self.discord_client.deinit();
    }

    fn handleTextMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, message_text: []const u8) void {
        log.info("Telegram -> Discord: Chat {d}, User {s}: {s}", .{ chat_id, display_name, message_text });
        log.info("User avatar_url: {?s}", .{user_info.avatar_url});

        const text = std.mem.trim(u8, message_text, " \t\n");
        if (text.len == 0) return;

        const escaped_message = helpers.escapeDiscordMarkdown(self.allocator, text) catch text;
        defer if (escaped_message.ptr != text.ptr) self.allocator.free(escaped_message);

        // TODO: Do we need fallback for formatting? e.g. incorrect markdown
        self.dc_webhook_executor.sendSpoofedMessage(escaped_message, display_name, user_info.avatar_url) catch |err| {
            log.info("Webhook failed: {}, falling back to regular message", .{err});
            self.sendRegularDiscordMessage(escaped_message, display_name);
        };
    }

    // fn handleAttachmentMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, attachment_info: telegram.AttachmentInfo) void {
    //     log.info("Telegram -> Discord: Chat {d}, User {s}: [attachment]", .{ chat_id, display_name });
    //
    //     if (attachment_info.url) |attachment_url| {
    //         if (self.config.debug_mode) {
    //             log.info("Sending attachment to Discord: {s}", .{attachment_url});
    //         }
    //
    //         switch (attachment_info.attachment_type) {
    //             .photo => {
    //                 self.dc_webhook_executor.sendSpoofedMessageWithImage(null, display_name, user_info.avatar_url, attachment_url) catch |err| {
    //                     log.info("Photo webhook failed: {}, falling back", .{err});
    //                     self.sendRegularDiscordMessage(attachment_url, display_name);
    //                 };
    //             },
    //             .sticker, .animation => {
    //                 self.dc_webhook_executor.sendSpoofedMessageWithAnimation(null, display_name, user_info.avatar_url, attachment_url) catch |err| {
    //                     log.info("Animation webhook failed: {}, falling back", .{err});
    //                     self.sendRegularDiscordMessage(attachment_url, display_name);
    //                 };
    //             },
    //             else => {
    //                 self.dc_webhook_executor.sendSpoofedMessage(attachment_url, display_name, user_info.avatar_url) catch |err| {
    //                     log.info("Attachment webhook failed: {}, falling back", .{err});
    //                     self.sendRegularDiscordMessage(attachment_url, display_name);
    //                 };
    //             },
    //         }
    //     } else {
    //         log.info("Attachment still downloading, will send when ready", .{});
    //     }
    // }

    // fn handleTextWithAttachmentMessage(self: *Self, chat_id: i64, display_name: []const u8, user_info: telegram.UserInfo, text: []const u8, attachment: telegram.AttachmentInfo) void {
    //     log.info("Telegram -> Discord: Chat {d}, User {s}: {s} [attachment]", .{ chat_id, display_name, text });
    //
    //     const escaped_caption = helpers.escapeDiscordMarkdown(self.allocator, text) catch text;
    //     defer if (escaped_caption.ptr != text.ptr) self.allocator.free(escaped_caption);
    //
    //     if (attachment.url) |attachment_url| {
    //         switch (attachment.attachment_type) {
    //             .photo => {
    //                 self.dc_webhook_executor.sendSpoofedMessageWithImage(escaped_caption, display_name, user_info.avatar_url, attachment_url) catch |err| {
    //                     log.info("Photo with caption webhook failed: {}", .{err});
    //                     self.sendFallbackMessage(escaped_caption, attachment_url, display_name);
    //                 };
    //             },
    //             .sticker, .animation => {
    //                 self.dc_webhook_executor.sendSpoofedMessageWithAnimation(escaped_caption, display_name, user_info.avatar_url, attachment_url) catch |err| {
    //                     log.info("Animation with caption webhook failed: {}", .{err});
    //                     self.sendFallbackMessage(escaped_caption, attachment_url, display_name);
    //                 };
    //             },
    //             else => {
    //                 const message_with_link = std.fmt.allocPrint(self.allocator, "{s}\n{s}", .{ escaped_caption, attachment_url }) catch {
    //                     log.info("Failed to format message with link and caption", .{});
    //                     return;
    //                 };
    //                 defer self.allocator.free(message_with_link);
    //
    //                 self.dc_webhook_executor.sendSpoofedMessage(message_with_link, display_name, user_info.avatar_url) catch |err| {
    //                     log.info("Attachment with caption webhook failed: {}", .{err});
    //                     self.sendRegularDiscordMessage(message_with_link, display_name);
    //                 };
    //             },
    //         }
    //     } else {
    //         log.info("Attachment with caption still downloading, will send when ready", .{});
    //     }
    // }

    // fn sendFallbackMessage(self: *Self, caption: []const u8, attachment_url: []const u8, display_name: []const u8) void {
    //     const fallback_message = std.fmt.allocPrint(self.allocator, "{s}\n{s}", .{ caption, attachment_url }) catch {
    //         log.info("Failed to format fallback message with caption", .{});
    //         return;
    //     };
    //     defer self.allocator.free(fallback_message);
    //
    //     self.sendRegularDiscordMessage(fallback_message, display_name);
    // }

    fn sendRegularDiscordMessage(self: *Self, message_text: []const u8, sender_name: []const u8) void {
        const formatted_message = std.fmt.allocPrint(self.allocator, "**{s}**: {s}", .{ sender_name, message_text }) catch {
            log.info("Failed to format message", .{});
            return;
        };
        defer self.allocator.free(formatted_message);

        var result = self.discord_client.session.api.sendMessage(self.config.discord_channel_id, .{
            .content = formatted_message,
        }) catch |err| {
            log.info("Failed to send message to Discord: {}", .{err});
            return;
        };
        defer result.deinit();

        if (self.config.debug_mode) {
            const m = result.value.unwrap();
            log.info("Sent to Discord: {?s}", .{m.content});
        }
    }

    fn onTelegramMessageEvent(ctx: ?*anyopaque, event: *const telegram_event.TelegramEvent) void {
        log.info("Telegram message event: {}", .{event});
        _ = ctx;

        // // TODO: This must be a filter on a library side, not here
        // if (event.* == .new_message) {
        //     const msg = &event.new_message;
        //     const self: *Self = @ptrCast(@alignCast(ctx.?));
        //     // Look up user info by sender_id
        //     if (self.telegram_client.user_cache.get(msg.sender_id)) |user_info| {
        //         // User info is available, call handler
        //         onTelegramMessage(@as(*anyopaque, self), msg.chat_id, user_info, msg.content);
        //     } else {
        //         // User info not in cache, fetch it
        //         var ctx_ptr = self.allocator.create(FetchUserInfoContext) catch {
        //             log.err("Failed to allocate FetchUserInfoContext", .{});
        //             return;
        //         };
        //         ctx_ptr.bridge = self;
        //         ctx_ptr.chat_id = msg.chat_id;
        //         ctx_ptr.sender_id = msg.sender_id;
        //         ctx_ptr.content = copyMessageContent(self.allocator, msg.content) catch |err| {
        //             log.err("Failed to copy message content: {}", .{err});
        //             self.allocator.destroy(ctx_ptr);
        //             return;
        //         };
        //         self.telegram_client.getUserWithCallback(msg.sender_id, onUserInfoFetched, ctx_ptr) catch |err| {
        //             log.err("Failed to fetch user info: {}", .{err});
        //             ctx_ptr.deinit(self.allocator);
        //             self.allocator.destroy(ctx_ptr);
        //         };
        //     }
        // }
    }
    //
    // const FetchUserInfoContext = struct {
    //     bridge: *Self,
    //     chat_id: i64,
    //     sender_id: i64,
    //     content: telegram.MessageContent,
    //     pub fn deinit(self: *FetchUserInfoContext, allocator: std.mem.Allocator) void {
    //         switch (self.content) {
    //             .attachment => |*attachment| attachment.deinit(allocator),
    //             .text_with_attachment => |*text_attachment| text_attachment.attachment.deinit(allocator),
    //             else => {},
    //         }
    //     }
    // };

    // fn copyMessageContent(allocator: std.mem.Allocator, content: telegram.MessageContent) !telegram.MessageContent {
    //     return switch (content) {
    //         .text => |txt| telegram.MessageContent{ .text = try allocator.dupe(u8, txt) },
    //         .attachment => |attachment| telegram.MessageContent{ .attachment = try attachment.deepCopy(allocator) },
    //         .text_with_attachment => |twa| telegram.MessageContent{ .text_with_attachment = .{
    //             .text = try allocator.dupe(u8, twa.text),
    //             .attachment = try twa.attachment.deepCopy(allocator),
    //         } },
    //     };
    // }

    // fn onUserInfoFetched(ctx: ?*anyopaque, result: telegram.CallbackResult) void {
    //     const fetch_ctx: *FetchUserInfoContext = @ptrCast(@alignCast(ctx.?));
    //     defer {
    //         fetch_ctx.deinit(fetch_ctx.bridge.allocator);
    //         fetch_ctx.bridge.allocator.destroy(fetch_ctx);
    //     }
    //     if (result == .success) {
    //         // User info should now be in the cache
    //         if (fetch_ctx.bridge.telegram_client.user_cache.get(fetch_ctx.sender_id)) |user_info| {
    //             onTelegramMessage(@as(*anyopaque, fetch_ctx.bridge), fetch_ctx.chat_id, user_info, fetch_ctx.content);
    //         } else {
    //             log.err("User info not found in cache after fetch", .{});
    //         }
    //     } else {
    //         log.err("Failed to fetch user info: {any}", .{result});
    //     }
    // }

    fn onTelegramAuthEvent(ctx: ?*anyopaque, event: *const telegram_event.TelegramEvent) void {
        const self: *Bridge = @ptrCast(@alignCast(ctx.?));
        const allocator = self.allocator;
        if (event.* != .auth_update) return;
        const auth = &event.auth_update;
        switch (auth.state) {
            .wait_tdlib_parameters => {
                log.info("TDLib is waiting for parameters. This should be handled by the library.", .{});
            },
            .wait_encryption_key => {
                log.info("TDLib is waiting for encryption key. Sending empty key.", .{});
                const req = "{\"@type\":\"checkDatabaseEncryptionKey\",\"encryption_key\":\"\"}";
                self.telegram_client.send(req) catch |err| {
                    log.err("Failed to send encryption key: {}", .{err});
                };
            },
            .wait_phone_number => {
                std.debug.print("Enter your phone number: ", .{});
                const phone = readInput(allocator) catch {
                    log.err("Failed to read phone number", .{});
                    return;
                };
                const req = std.fmt.allocPrint(allocator, "{{\"@type\":\"setAuthenticationPhoneNumber\",\"phone_number\":\"{s}\"}}", .{phone}) catch {
                    log.err("Failed to format phone request", .{});
                    return;
                };
                defer allocator.free(req);
                self.telegram_client.send(req) catch |err| {
                    log.err("Failed to send phone: {}", .{err});
                };
            },
            .wait_code => |wc| {
                if (wc.code_info) |ci| {
                    if (ci.type) |t| std.debug.print("Code type: {s}\n", .{t});
                    if (ci.length) |l| std.debug.print("Code length: {d}\n", .{l});
                    if (ci.next_type) |nt| std.debug.print("Next code type: {s}\n", .{nt});
                }
                std.debug.print("Enter the code you received: ", .{});
                const code = readInput(allocator) catch {
                    log.err("Failed to read code", .{});
                    return;
                };
                const req = std.fmt.allocPrint(allocator, "{{\"@type\":\"checkAuthenticationCode\",\"code\":\"{s}\"}}", .{code}) catch {
                    log.err("Failed to format code request", .{});
                    return;
                };
                defer allocator.free(req);
                self.telegram_client.send(req) catch |err| {
                    log.err("Failed to send code: {}", .{err});
                };
            },
            .wait_password => |wp| {
                if (wp.password_hint) |hint| std.debug.print("Password hint: {s}\n", .{hint});
                if (wp.has_recovery_email_address) |has_email| std.debug.print("Has recovery email: {}\n", .{has_email});
                if (wp.recovery_email_address_pattern) |pat| std.debug.print("Recovery email pattern: {s}\n", .{pat});
                std.debug.print("Enter your 2FA password: ", .{});
                const password = readInput(allocator) catch {
                    log.err("Failed to read password", .{});
                    return;
                };
                const req = std.fmt.allocPrint(allocator, "{{\"@type\":\"checkAuthenticationPassword\",\"password\":\"{s}\"}}", .{password}) catch {
                    log.err("Failed to format password request", .{});
                    return;
                };
                defer allocator.free(req);
                self.telegram_client.send(req) catch |err| {
                    log.err("Failed to send password: {}", .{err});
                };
            },
            .ready => {
                log.info("Telegram client authorized!", .{});
            },
            .logging_out => {
                log.info("Telegram client logging out...", .{});
            },
            .closing => {
                log.info("Telegram client closing...", .{});
            },
            .closed => {
                log.info("Telegram client closed.", .{});
            },
            .unknown => |s| {
                log.info("Unknown Telegram auth state: {s}", .{s});
            },
        }
    }

    fn readInput(allocator: std.mem.Allocator) ![]u8 {
        const stdin = std.io.getStdIn().reader();
        const input = try stdin.readUntilDelimiterAlloc(allocator, '\n', 1024);
        const trimmed = std.mem.trim(u8, input, " \t\r\n");
        defer allocator.free(input);
        return try allocator.dupe(u8, trimmed);
    }

    pub fn start(self: *Self) !void {
        log.info("\n=== Starting Zefxi Bridge ===", .{});
        log.info("Telegram Chat ID: {?d}", .{self.config.telegram_chat_id});
        log.info("Discord Channel ID: {s}", .{self.config.discord_channel_id});
        // log.info("File Server Port: {d}", .{self.config.file_server_port});
        log.info("Debug mode: {}", .{self.config.debug_mode});
        log.info("Press Ctrl+C to exit\n", .{});

        // Set up handlers and connections
        try self.telegram_client.addEventHandler(.new_message, onTelegramMessageEvent, self);
        try self.telegram_client.addEventHandler(.auth_update, onTelegramAuthEvent, self);
        self.discord_client.setMessageHandler(self, &onDiscordMessage);
        self.discord_client.setWebhookExecutor(self.dc_webhook_executor);

        // TODO: Add support for files later
        // Start services
        // log.info("Starting file HTTP server...", .{});
        // const attachments_server_thread = try std.Thread.spawn(.{}, startAttachmentsServer, .{&self.attachments_server});
        // defer attachments_server_thread.join();

        log.info("Starting Telegram client...", .{});
        try self.telegram_client.start();

        log.info("Starting Discord client...", .{});
        const discord_thread = try std.Thread.spawn(.{}, startDiscordClient, .{&self.discord_client});
        defer discord_thread.join();

        log.info("Bridge is now running!", .{});
        while (true) {
            if (!try self.telegram_client.tick()) {
                log.info("Telegram client tick failed, exiting...", .{});
                break;
            }
            std.time.sleep(20 * std.time.ns_per_ms);
        }
    }

    fn startDiscordClient(client: *discord.DiscordClient) void {
        client.start() catch |err| {
            log.info("Discord client error: {}", .{err});
        };
    }

    // fn startAttachmentsServer(server: *http_server.HttpServer) void {
    //     server.start() catch |err| {
    //         log.info("File server error: {}", .{err});
    //     };
    // }

    fn onDiscordMessage(ctx: *anyopaque, channel_id: []const u8, user_id: []const u8, username: []const u8, message_text: []const u8, attachments: ?[]const Discord.Attachment) void {
        _ = ctx;
        _ = channel_id;
        _ = user_id;
        _ = attachments;
        // TODO: Implement Discord message handling logic or restore previous implementation.
        std.log.info("Received Discord message from {s}: {s}", .{ username, message_text });
    }
};

// Free function handler for Telegram messages
// fn onTelegramMessage(ctx: *anyopaque, chat_id: i64, user_info: telegram.UserInfo, content: telegram.MessageContent) void {
//     const self: *Bridge = @ptrCast(@alignCast(ctx));
//
//     // Debug user info before getting display name
//     log.info("Processing message from user {d}", .{user_info.user_id});
//     log.info("User first_name: '{s}'", .{user_info.first_name});
//     if (user_info.last_name) |lname| {
//         log.info("User last_name: '{s}'", .{lname});
//     }
//
//     const display_name = user_info.getDisplayName(self.allocator) catch |err| {
//         log.info("Failed to get display name for user {d}: {}", .{ user_info.user_id, err });
//         return;
//     };
//     defer self.allocator.free(display_name);
//
//     log.info("Generated display_name: '{s}'", .{display_name});
//
//     if (self.config.debug_mode) {
//         log.info("Processing message from user {s} ({d})", .{ display_name, user_info.user_id });
//     }
//
//     switch (content) {
//         .text => |message_text| {
//             self.handleTextMessage(chat_id, display_name, user_info, message_text);
//         },
//         // .attachment => |attachment_info| {
//         //     self.handleAttachmentMessage(chat_id, display_name, user_info, attachment_info);
//         // },
//         .text_with_attachment => |text_attachment| {
//             self.handleTextWithAttachmentMessage(chat_id, display_name, user_info, text_attachment.text, text_attachment.attachment);
//         },
//     }
// }

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}).init;
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    var config_arena = std.heap.ArenaAllocator.init(allocator);
    defer config_arena.deinit();

    const bridge_config = try Config.parseConfig(config_arena.allocator());
    defer bridge_config.deinit(config_arena.allocator());

    var bridge = Bridge.init(allocator, bridge_config) catch |err| {
        log.err("Failed to initialize bridge: {}", .{err});
        return;
    };
    defer bridge.deinit();

    try bridge.start();
    log.info("\nBridge session completed!", .{});
}
