const std = @import("std");
pub const Discord = @import("discord");
const log = std.log.scoped(.discord);
const Allocator = std.mem.Allocator;

pub const DiscordError = error{
    InvalidToken,
    ConnectionFailed,
    AuthenticationFailed,
    WebhookExecutionFailed,
};

pub const MessageHandler = *const fn (ctx: *anyopaque, channel_id: []const u8, user_id: []const u8, username: []const u8, message_text: []const u8, attachments: ?[]const Discord.Attachment) void;

// TODO: Why is everything optional?
const WebhookExecutePayload = struct {
    content: ?[]const u8 = null,
    username: ?[]const u8 = null,
    avatar_url: ?[]const u8 = null,
    embeds: ?[]Discord.Embed = null,
};

pub const WebhookExecutor = struct {
    allocator: Allocator,
    webhook_url: []const u8,
    debug_mode: bool,

    const Self = @This();

    pub fn init(allocator: Allocator, webhook_url: []const u8, debug_mode: bool) Self {
        return Self{
            .allocator = allocator,
            .webhook_url = webhook_url,
            .debug_mode = debug_mode,
        };
    }

    fn executeWebhook(self: *Self, payload: WebhookExecutePayload) !void {
        var client = std.http.Client{ .allocator = self.allocator };
        defer client.deinit();

        var json_payload = std.ArrayList(u8).init(self.allocator);
        defer json_payload.deinit();

        try std.json.stringify(payload, .{
            .emit_null_optional_fields = false,
        }, json_payload.writer());

        if (self.debug_mode) {
            log.info("Sending payload: {s}\n", .{json_payload.items});
        }

        var headers = std.ArrayList(std.http.Header).init(self.allocator);
        defer headers.deinit();

        try headers.append(.{ .name = "Content-Type", .value = "application/json" });
        try headers.append(.{ .name = "User-Agent", .value = "DiscordBot (zefxi, 0.1.0)" });

        var response_body = std.ArrayList(u8).init(self.allocator);
        defer response_body.deinit();

        const fetch_result = client.fetch(.{
            .location = .{ .url = self.webhook_url },
            .method = .POST,
            .payload = json_payload.items,
            .extra_headers = try headers.toOwnedSlice(),
            .response_storage = .{ .dynamic = &response_body },
        }) catch |err| {
            log.err("Request failed: {}\n", .{err});
            return DiscordError.WebhookExecutionFailed;
        };

        if (self.debug_mode) {
            log.info("Response status: {}\n", .{fetch_result.status});
            if (response_body.items.len > 0) {
                log.info("Response body: {s}\n", .{response_body.items});
            }
        }

        switch (fetch_result.status.class()) {
            .success => {
                if (self.debug_mode) {
                    log.info("Message sent successfully\n", .{});
                }
            },
            else => {
                log.err("Request failed with status: {}\n", .{fetch_result.status});
                if (response_body.items.len > 0) {
                    log.err("Error response: {s}\n", .{response_body.items});
                }
                return DiscordError.WebhookExecutionFailed;
            },
        }
    }

    pub fn sendSpoofedMessage(self: *Self, content: []const u8, username: ?[]const u8, avatar_url: ?[]const u8) !void {
        if (self.debug_mode) {
            log.info("Sending spoofed message:\n", .{});
            log.info("   Content: {s}\n", .{content});
            log.info("   Username: {?s}\n", .{username});
            log.info("   Avatar URL: {?s}\n", .{avatar_url});
        }

        const payload = WebhookExecutePayload{
            .content = content,
            .username = username,
            .avatar_url = avatar_url,
        };

        try self.executeWebhook(payload);
    }

    pub fn sendSpoofedMessageWithImage(self: *Self, content: ?[]const u8, username: ?[]const u8, avatar_url: ?[]const u8, image_url: []const u8) !void {
        const embed = Discord.Embed{
            .image = .{ .url = image_url },
            .description = content,
        };

        const payload = WebhookExecutePayload{
            .content = null,
            .username = username,
            .avatar_url = avatar_url,
            .embeds = @constCast(&[_]Discord.Embed{embed}),
        };

        try self.executeWebhook(payload);
    }

    pub fn sendSpoofedMessageWithAnimation(self: *Self, content: ?[]const u8, username: ?[]const u8, avatar_url: ?[]const u8, animation_url: []const u8) !void {
        const message_content = if (content) |caption|
            try std.fmt.allocPrint(self.allocator, "{s}\n{s}", .{ caption, animation_url })
        else
            try std.fmt.allocPrint(self.allocator, "{s}", .{animation_url});
        defer self.allocator.free(message_content);

        const payload = WebhookExecutePayload{
            .content = message_content,
            .username = username,
            .avatar_url = avatar_url,
        };

        try self.executeWebhook(payload);
    }
};

var global_client: ?*DiscordClient = null;

pub const DiscordClient = struct {
    allocator: Allocator,
    session: *Discord.Session,
    token: []const u8,
    target_server_id: Discord.Snowflake,
    target_channel_id: Discord.Snowflake,
    message_handler: ?MessageHandler,
    message_handler_ctx: ?*anyopaque,
    debug_mode: bool,
    webhook_executor: ?WebhookExecutor,

    const Self = @This();

    pub fn init(allocator: Allocator, token: []const u8, target_server_id: Discord.Snowflake, target_channel_id: Discord.Snowflake, debug_mode: bool) !Self {
        const session = try allocator.create(Discord.Session);
        session.* = Discord.init(allocator);

        return Self{
            .allocator = allocator,
            .session = session,
            .token = token,
            .target_server_id = target_server_id,
            .target_channel_id = target_channel_id,
            .message_handler = null,
            .message_handler_ctx = null,
            .debug_mode = debug_mode,
            .webhook_executor = null,
        };
    }

    pub fn deinit(self: *Self) void {
        self.session.deinit();
        self.allocator.destroy(self.session);
        if (global_client == self) {
            global_client = null;
        }
    }

    pub fn setMessageHandler(self: *Self, ctx: *anyopaque, handler: MessageHandler) void {
        self.message_handler = handler;
        self.message_handler_ctx = ctx;
    }

    pub fn setWebhookExecutor(self: *Self, webhook_executor: WebhookExecutor) void {
        self.webhook_executor = webhook_executor;
    }

    fn ready(_: *Discord.Shard, payload: Discord.Ready) !void {
        log.info("Logged in as {s}\n", .{payload.user.username});
    }

    fn messageCreate(_: *Discord.Shard, message: Discord.Message) !void {
        const client = global_client orelse return;

        if (message.author.bot orelse false) return;

        if (message.channel_id != client.target_channel_id) return;

        const content = message.content orelse "";

        const display_name = if (message.author.global_name) |global_name|
            global_name
        else
            message.author.username;

        const has_attachments = message.attachments.len > 0;

        if (has_attachments) {
            log.info("NEW MESSAGE: Channel {}, User {s}: {s} [with {} attachment(s)]\n", .{ message.channel_id, display_name, content, message.attachments.len });
        } else {
            log.info("NEW MESSAGE: Channel {}, User {s}: {s}\n", .{ message.channel_id, display_name, content });
        }

        if (client.message_handler) |handler| {
            if (client.message_handler_ctx) |ctx| {
                const channel_id_str = std.fmt.allocPrint(client.allocator, "{}", .{message.channel_id}) catch return;
                defer client.allocator.free(channel_id_str);

                const user_id_str = std.fmt.allocPrint(client.allocator, "{}", .{message.author.id}) catch return;
                defer client.allocator.free(user_id_str);

                handler(ctx, channel_id_str, user_id_str, display_name, content, if (has_attachments) message.attachments else null);
            }
        }
    }

    pub fn start(self: *Self) !void {
        const intents = comptime blk: {
            var bits: Discord.Intents = .{};
            bits.Guilds = true;
            bits.GuildMessages = true;
            bits.GuildMembers = true;
            bits.MessageContent = true;
            break :blk bits;
        };

        log.info("Starting Discord client...\n", .{});

        global_client = self;

        try self.session.start(.{
            .intents = intents,
            .authorization = self.token,
            .run = .{
                .message_create = &messageCreate,
                .ready = &ready,
            },
            .log = if (self.debug_mode) Discord.Internal.Log.yes else Discord.Internal.Log.no,
            .options = .{},
            .cache = Discord.cache.CacheTables(Discord.cache.TableTemplate{}).defaults(self.allocator),
        });
    }
};
