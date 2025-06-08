const std = @import("std");
const Discord = @import("discord");
const print = std.debug.print;
const Allocator = std.mem.Allocator;

pub const DiscordError = error{
    InvalidToken,
    ConnectionFailed,
    AuthenticationFailed,
};

pub const MessageHandler = *const fn (channel_id: u64, user_id: u64, username: []const u8, message_text: []const u8) void;

// Global state for the Discord client (needed for callbacks)
var global_client: ?*DiscordClient = null;

pub const DiscordClient = struct {
    allocator: Allocator,
    session: *Discord.Session,
    token: []const u8,
    target_server_id: ?u64,
    target_channel_id: ?u64,
    message_handler: ?MessageHandler,
    debug_mode: bool,
    
    const Self = @This();

    pub fn init(allocator: Allocator, token: []const u8, target_server_id: ?u64, target_channel_id: ?u64, debug_mode: bool) !Self {
        const session = try allocator.create(Discord.Session);
        session.* = Discord.init(allocator);
        
        return Self{
            .allocator = allocator,
            .session = session,
            .token = token,
            .target_server_id = target_server_id,
            .target_channel_id = target_channel_id,
            .message_handler = null,
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

    pub fn setMessageHandler(self: *Self, handler: MessageHandler) void {
        self.message_handler = handler;
    }

    fn ready(_: *Discord.Shard, payload: Discord.Ready) !void {
        print("[Discord] Logged in as {s}\n", .{payload.user.username});
    }

    fn messageCreate(_: *Discord.Shard, message: Discord.Message) !void {
        const client = global_client orelse return;
        
        // Skip messages from bots
        if (message.author.bot orelse false) return;
        
        // Filter by target channel if specified
        if (client.target_channel_id) |target_id| {
            if (message.channel_id != target_id) return;
        }
        
        const content = message.content orelse "";
        const username = message.author.username;
        
        print("[Discord] NEW MESSAGE: Channel {d}, User {s} ({d}): {s}\n", .{ 
            message.channel_id, username, message.author.id, content 
        });
        
        // Call the message handler if set
        if (client.message_handler) |handler| {
            handler(message.channel_id, message.author.id, username, content);
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