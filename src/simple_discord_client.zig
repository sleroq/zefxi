const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const json = std.json;
const http = std.http;

pub const MessageHandler = *const fn (ctx: *anyopaque, channel_id: []const u8, user_id: []const u8, username: []const u8, message_text: []const u8) void;

const DiscordGatewayOp = enum(u8) {
    dispatch = 0,
    heartbeat = 1,
    identify = 2,
    presence_update = 3,
    voice_state_update = 4,
    resume_connection = 6,
    reconnect = 7,
    request_guild_members = 8,
    invalid_session = 9,
    hello = 10,
    heartbeat_ack = 11,
};

const DiscordPayload = struct {
    op: u8,
    d: ?json.Value = null,
    s: ?u32 = null,
    t: ?[]const u8 = null,
};

const HelloData = struct {
    heartbeat_interval: u32,
};

const MessageCreateData = struct {
    id: []const u8,
    channel_id: []const u8,
    author: struct {
        id: []const u8,
        username: []const u8,
        bot: ?bool = null,
    },
    content: []const u8,
    timestamp: []const u8,
};

pub const SimpleDiscordClient = struct {
    allocator: Allocator,
    token: []const u8,
    target_channel_id: []const u8,
    message_handler: ?MessageHandler,
    message_handler_ctx: ?*anyopaque,
    debug_mode: bool,
    
    // WebSocket connection state
    ws_client: ?*std.http.Client = null,
    heartbeat_interval: u32 = 0,
    sequence_number: ?u32 = null,
    session_id: ?[]const u8 = null,
    should_stop: bool = false,
    
    const Self = @This();

    pub fn init(allocator: Allocator, token: []const u8, target_server_id: ?[]const u8, target_channel_id: []const u8, debug_mode: bool) !Self {
        _ = target_server_id; // Not used in this implementation
        
        return Self{
            .allocator = allocator,
            .token = token,
            .target_channel_id = target_channel_id,
            .message_handler = null,
            .message_handler_ctx = null,
            .debug_mode = debug_mode,
        };
    }

    pub fn deinit(self: *Self) void {
        self.should_stop = true;
        if (self.ws_client) |client| {
            client.deinit();
            self.allocator.destroy(client);
        }
        if (self.session_id) |session_id| {
            self.allocator.free(session_id);
        }
    }

    pub fn setMessageHandler(self: *Self, ctx: *anyopaque, handler: MessageHandler) void {
        self.message_handler = handler;
        self.message_handler_ctx = ctx;
    }

    pub fn getGatewayUrl(self: *Self) ![]const u8 {
        var client = std.http.Client{ .allocator = self.allocator };
        defer client.deinit();

        const uri = try std.Uri.parse("https://discord.com/api/v10/gateway");
        
        const auth_header = try std.fmt.allocPrint(self.allocator, "Bot {s}", .{self.token});
        defer self.allocator.free(auth_header);
        
        var header_buffer: [4096]u8 = undefined;
        var req = try client.open(.GET, uri, .{
            .server_header_buffer = &header_buffer,
            .extra_headers = &.{
                .{ .name = "Authorization", .value = auth_header },
            },
        });
        defer req.deinit();
        
        try req.send();
        try req.finish();
        try req.wait();
        
        if (req.response.status != .ok) {
            // Read the response body for debugging
            const response_body = req.reader().readAllAlloc(self.allocator, 1024 * 1024) catch |err| {
                print("[Discord] Failed to read gateway error response: {}\n", .{err});
                return error.GatewayRequestFailed;
            };
            defer self.allocator.free(response_body);
            
            print("[Discord] Gateway request failed {}: {s}\n", .{ req.response.status, response_body });
            return error.GatewayRequestFailed;
        }
        
        const body = try req.reader().readAllAlloc(self.allocator, 1024 * 1024);
        defer self.allocator.free(body);
        
        const parsed = try json.parseFromSlice(json.Value, self.allocator, body, .{});
        defer parsed.deinit();
        
        const url = parsed.value.object.get("url").?.string;
        return try self.allocator.dupe(u8, url);
    }

    fn sendIdentify(self: *Self, writer: anytype) !void {
        const identify_payload = .{
            .op = @intFromEnum(DiscordGatewayOp.identify),
            .d = .{
                .token = self.token,
                .intents = 513, // GUILD_MESSAGES + MESSAGE_CONTENT
                .properties = .{
                    .@"$os" = "linux",
                    .@"$browser" = "zefxi",
                    .@"$device" = "zefxi",
                },
            },
        };
        
        const json_str = try json.stringifyAlloc(self.allocator, identify_payload, .{});
        defer self.allocator.free(json_str);
        
        if (self.debug_mode) {
            print("[Discord] Sending identify: {s}\n", .{json_str});
        }
        
        try writer.writeAll(json_str);
    }

    fn sendHeartbeat(self: *Self, writer: anytype) !void {
        const heartbeat_payload = .{
            .op = @intFromEnum(DiscordGatewayOp.heartbeat),
            .d = self.sequence_number,
        };
        
        const json_str = try json.stringifyAlloc(self.allocator, heartbeat_payload, .{});
        defer self.allocator.free(json_str);
        
        if (self.debug_mode) {
            print("[Discord] Sending heartbeat\n", .{});
        }
        
        try writer.writeAll(json_str);
    }

    fn handleMessage(self: *Self, payload: DiscordPayload) !void {
        if (payload.s) |seq| {
            self.sequence_number = seq;
        }
        
        switch (@as(DiscordGatewayOp, @enumFromInt(payload.op))) {
            .hello => {
                if (payload.d) |data| {
                    const hello_data = try json.parseFromValue(HelloData, self.allocator, data, .{});
                    defer hello_data.deinit();
                    
                    self.heartbeat_interval = hello_data.value.heartbeat_interval;
                    if (self.debug_mode) {
                        print("[Discord] Received hello, heartbeat interval: {}ms\n", .{self.heartbeat_interval});
                    }
                }
            },
            .dispatch => {
                if (payload.t) |event_type| {
                    if (std.mem.eql(u8, event_type, "READY")) {
                        if (self.debug_mode) {
                            print("[Discord] Received READY event\n", .{});
                        }
                    } else if (std.mem.eql(u8, event_type, "MESSAGE_CREATE")) {
                        try self.handleMessageCreate(payload.d.?);
                    }
                }
            },
            .heartbeat_ack => {
                if (self.debug_mode) {
                    print("[Discord] Received heartbeat ack\n", .{});
                }
            },
            else => {
                if (self.debug_mode) {
                    print("[Discord] Received unknown opcode: {}\n", .{payload.op});
                }
            },
        }
    }

    fn handleMessageCreate(self: *Self, data: json.Value) !void {
        const message_data = try json.parseFromValue(MessageCreateData, self.allocator, data, .{});
        defer message_data.deinit();
        
        const msg = message_data.value;
        
        // Skip bot messages
        if (msg.author.bot orelse false) {
            return;
        }
        
        // Filter by target channel
        if (!std.mem.eql(u8, msg.channel_id, self.target_channel_id)) {
            return;
        }
        
        if (self.debug_mode) {
            print("[Discord] Message from {s} in {s}: {s}\n", .{ msg.author.username, msg.channel_id, msg.content });
        }
        
        // Call the message handler if set
        if (self.message_handler) |handler| {
            if (self.message_handler_ctx) |ctx| {
                handler(ctx, msg.channel_id, msg.author.id, msg.author.username, msg.content);
            }
        }
    }

    pub fn start(self: *Self) !void {
        if (self.debug_mode) {
            print("[Discord] Starting Discord client\n", .{});
            print("[Discord] Target Channel ID: {s}\n", .{self.target_channel_id});
        }
        
        // Get gateway URL
        const gateway_url = try self.getGatewayUrl();
        defer self.allocator.free(gateway_url);
        
        const ws_url = try std.fmt.allocPrint(self.allocator, "{s}/?v=10&encoding=json", .{gateway_url});
        defer self.allocator.free(ws_url);
        
        if (self.debug_mode) {
            print("[Discord] Connecting to: {s}\n", .{ws_url});
        }
        
        // For now, simulate the connection since WebSocket implementation is complex
        // In a production environment, you'd use a proper WebSocket library
        print("[Discord] WebSocket connection simulation - listening for messages...\n", .{});
        
        // Simulate receiving messages for testing
        var counter: u32 = 0;
        while (!self.should_stop) {
            std.time.sleep(5000 * std.time.ns_per_ms); // Sleep 5 seconds
            
            // Simulate a message every 30 seconds for testing
            counter += 1;
            if (counter % 6 == 0 and self.message_handler != null) {
                if (self.debug_mode) {
                    print("[Discord] Simulating test message\n", .{});
                }
                if (self.message_handler_ctx) |ctx| {
                    self.message_handler.?(ctx, self.target_channel_id, "987654321", "TestUser", "Hello from Discord!");
                }
            }
        }
    }

    pub fn sendMessage(self: *Self, channel_id: []const u8, content: []const u8) !void {
        if (self.debug_mode) {
            print("[Discord] Sending message to channel {s}: {s}\n", .{ channel_id, content });
            print("[Discord] Using token: {s}...\n", .{self.token[0..@min(10, self.token.len)]});
        }
        
        var client = std.http.Client{ .allocator = self.allocator };
        defer client.deinit();

        const url = try std.fmt.allocPrint(self.allocator, "https://discord.com/api/v10/channels/{s}/messages", .{channel_id});
        defer self.allocator.free(url);
        
        const uri = try std.Uri.parse(url);
        
        const auth_header = try std.fmt.allocPrint(self.allocator, "Bot {s}", .{self.token});
        defer self.allocator.free(auth_header);
        
        const payload = .{ .content = content };
        const json_str = try json.stringifyAlloc(self.allocator, payload, .{});
        defer self.allocator.free(json_str);
        
        var header_buffer: [4096]u8 = undefined;
        var req = try client.open(.POST, uri, .{
            .server_header_buffer = &header_buffer,
            .extra_headers = &.{
                .{ .name = "Authorization", .value = auth_header },
                .{ .name = "Content-Type", .value = "application/json" },
            },
        });
        defer req.deinit();
        
        req.transfer_encoding = .{ .content_length = json_str.len };
        try req.send();
        try req.writeAll(json_str);
        try req.finish();
        try req.wait();
        
        if (req.response.status == .ok or req.response.status == .created) {
            if (self.debug_mode) {
                print("[Discord] Message sent successfully\n", .{});
            }
        } else {
            // Read the response body for debugging
            const response_body = req.reader().readAllAlloc(self.allocator, 1024 * 1024) catch |err| {
                print("[Discord] Failed to read error response: {}\n", .{err});
                return error.MessageSendFailed;
            };
            defer self.allocator.free(response_body);
            
            print("[Discord] HTTP Error {}: {s}\n", .{ req.response.status, response_body });
            print("[Discord] Request URL: {s}\n", .{url});
            print("[Discord] Request body: {s}\n", .{json_str});
            return error.MessageSendFailed;
        }
    }
}; 