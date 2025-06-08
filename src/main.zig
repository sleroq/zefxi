const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const telegram = @import("telegram.zig");
const discord_client = @import("discord_client.zig");

const BridgeConfig = struct {
    debug_mode: bool = false,
    telegram_api_id: i32,
    telegram_api_hash: []const u8,
    telegram_chat_id: ?i64,
    discord_token: []const u8,
    discord_server_id: ?u64,
    discord_channel_id: ?u64,
};

const Bridge = struct {
    allocator: Allocator,
    config: BridgeConfig,
    telegram_client: telegram.TelegramClient,
    discord_client: discord_client.DiscordClient,
    
    const Self = @This();

    pub fn init(allocator: Allocator, config: BridgeConfig) !Self {
        var telegram_config = telegram.Config{
            .debug_mode = config.debug_mode,
            .receive_timeout = 0.1, // Shorter timeout for better responsiveness
        };
        
        var tg_client = try telegram.TelegramClient.init(
            allocator,
            config.telegram_api_id,
            config.telegram_api_hash,
            config.telegram_chat_id,
            telegram_config
        );
        
        var dc_client = try discord_client.DiscordClient.init(
            allocator,
            config.discord_token,
            config.discord_server_id,
            config.discord_channel_id,
            config.debug_mode
        );
        
        return Self{
            .allocator = allocator,
            .config = config,
            .telegram_client = tg_client,
            .discord_client = dc_client,
        };
    }

    pub fn deinit(self: *Self) void {
        self.telegram_client.deinit();
        self.discord_client.deinit();
    }

    fn onTelegramMessage(chat_id: i64, user_id: i64, message_text: []const u8) void {
        print("[Bridge] Telegram -> Discord: Chat {d}, User {d}: {s}\n", .{ chat_id, user_id, message_text });
        // TODO: Forward to Discord
    }

    fn onDiscordMessage(channel_id: u64, user_id: u64, username: []const u8, message_text: []const u8) void {
        print("[Bridge] Discord -> Telegram: Channel {d}, User {s} ({d}): {s}\n", .{ channel_id, username, user_id, message_text });
        // TODO: Forward to Telegram
    }

    pub fn start(self: *Self) !void {
        print("\n=== Starting Zefxi Bridge ===\n", .{});
        print("Telegram Chat ID: {?d}\n", .{self.config.telegram_chat_id});
        print("Discord Channel ID: {?d}\n", .{self.config.discord_channel_id});
        print("Debug mode: {}\n", .{self.config.debug_mode});
        print("Press Ctrl+C to exit\n\n", .{});

        // Set up message handlers
        self.telegram_client.setMessageHandler(&onTelegramMessage);
        self.discord_client.setMessageHandler(&onDiscordMessage);

        // Start Telegram client
        print("[Bridge] Starting Telegram client...\n", .{});
        try self.telegram_client.start();

        // Start Discord client in a separate thread
        print("[Bridge] Starting Discord client...\n", .{});
        const discord_thread = try std.Thread.spawn(.{}, startDiscordClient, .{&self.discord_client});
        defer discord_thread.join();

        // Main event loop for Telegram
        print("[Bridge] Bridge is now running!\n", .{});
        while (true) {
            if (!try self.telegram_client.tick()) {
                break;
            }
            
            // Small delay to prevent busy waiting
            std.time.sleep(10 * std.time.ns_per_ms);
        }
    }

    fn startDiscordClient(client: *discord_client.DiscordClient) void {
        client.start() catch |err| {
            print("[Bridge] Discord client error: {}\n", .{err});
        };
    }
};

fn parseEnvU64(allocator: Allocator, env_var: []const u8) !?u64 {
    if (std.process.getEnvVarOwned(allocator, env_var)) |value| {
        defer allocator.free(value);
        return std.fmt.parseInt(u64, value, 10) catch |err| {
            print("Warning: Invalid {s} format: {}\n", .{ env_var, err });
            return null;
        };
    } else |_| {
        return null;
    }
}

fn parseEnvI64(allocator: Allocator, env_var: []const u8) !?i64 {
    if (std.process.getEnvVarOwned(allocator, env_var)) |value| {
        defer allocator.free(value);
        return std.fmt.parseInt(i64, value, 10) catch |err| {
            print("Warning: Invalid {s} format: {}\n", .{ env_var, err });
            return null;
        };
    } else |_| {
        return null;
    }
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

    // Get optional target IDs
    const telegram_chat_id = try parseEnvI64(allocator, "TELEGRAM_CHAT_ID");
    const discord_server_id = try parseEnvU64(allocator, "DISCORD_SERVER");
    const discord_channel_id = try parseEnvU64(allocator, "DISCORD_CHANNEL");

    // Get debug mode
    var debug_mode = false;
    if (std.process.getEnvVarOwned(allocator, "DEBUG")) |debug_str| {
        defer allocator.free(debug_str);
        debug_mode = std.mem.eql(u8, debug_str, "1") or std.mem.eql(u8, debug_str, "true");
    } else |_| {
        // Default to false
    }

    const config = BridgeConfig{
        .debug_mode = debug_mode,
        .telegram_api_id = api_id,
        .telegram_api_hash = api_hash,
        .telegram_chat_id = telegram_chat_id,
        .discord_token = discord_token,
        .discord_server_id = discord_server_id,
        .discord_channel_id = discord_channel_id,
    };

    var bridge = Bridge.init(allocator, config) catch |err| {
        print("Failed to initialize bridge: {}\n", .{err});
        return;
    };
    defer bridge.deinit();

    try bridge.start();

    print("\nBridge session completed!\n", .{});
} 