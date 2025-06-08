const std = @import("std");
pub const Discord = @import("discord");
const print = std.debug.print;
const Allocator = std.mem.Allocator;

pub const DiscordError = error{
    InvalidToken,
    ConnectionFailed,
    AuthenticationFailed,
};

pub const MessageHandler = *const fn (ctx: *anyopaque, channel_id: Discord.Snowflake, user_id: Discord.Snowflake, username: []const u8, message_text: []const u8) void;

// Global state for the Discord client (needed for callbacks)
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

    fn ready(_: *Discord.Shard, payload: Discord.Ready) !void {
        print("[Discord] Logged in as {s}\n", .{payload.user.username});
    }

    fn messageCreate(_: *Discord.Shard, message: Discord.Message) !void {
        const client = global_client orelse return;
        
        // Skip messages from bots
        // if (message.author.bot orelse false) return;
        
        if (message.channel_id != client.target_channel_id) return;
        
        const content = message.content orelse "";
        const username = message.author.username;
        
        print("[Discord] NEW MESSAGE: Channel {}, User {s} ({}): {s}\n", .{ 
            message.channel_id, username, message.author.id, content 
        });
        
        // Call the message handler if set
        if (client.message_handler) |handler| {
            if (client.message_handler_ctx) |ctx| {
                handler(ctx, message.channel_id, message.author.id, username, content);
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

        print("[Discord] Starting Discord client...\n", .{});
        
        // Set the global client reference for the callback
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