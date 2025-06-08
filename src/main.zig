const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const telegram = @import("telegram.zig");
const discord = @import("discord.zig");
const Discord = @import("discord");

const BridgeConfig = struct {
    debug_mode: bool = false,
    telegram_api_id: i32,
    telegram_api_hash: []const u8,
    telegram_chat_id: ?i64,
    discord_token: []const u8,
    discord_server_id: Discord.Snowflake,
    discord_channel_id: Discord.Snowflake,
};

const Bridge = struct {
    allocator: Allocator,
    config: BridgeConfig,
    telegram_client: telegram.TelegramClient,
    discord_client: discord.DiscordClient,
    
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
            telegram_config
        );
        
        const dc_client = try discord.DiscordClient.init(
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

    fn onTelegramMessage(ctx: *anyopaque, chat_id: i64, user_id: i64, message_text: []const u8) void {
        const self: *Self = @ptrCast(@alignCast(ctx));
        print("[Bridge] Telegram -> Discord: Chat {d}, User {d}: {s}\n", .{ chat_id, user_id, message_text });
        
        // Skip empty messages
        if (message_text.len == 0) {
            return;
        }
        
        // Forward to Discord
        const channel_id = self.config.discord_channel_id;
        
        // Escape Discord markdown characters in the message
        const escaped_message = self.escapeDiscordMarkdown(message_text) catch message_text;
        defer if (escaped_message.ptr != message_text.ptr) self.allocator.free(escaped_message);
        
        var result = self.discord_client.session.api.sendMessage(channel_id, .{
            .content = escaped_message,
        }) catch |err| {
            print("[Bridge] Failed to send message to Discord: {}\n", .{err});
            return;
        };

        defer result.deinit();

        const m = result.value.unwrap();
        std.debug.print("sent: {?s}\n", .{m.content});
    }

    fn onDiscordMessage(ctx: *anyopaque, channel_id: []const u8, user_id: []const u8, username: []const u8, message_text: []const u8) void {
        const self: *Self = @ptrCast(@alignCast(ctx));
        print("[Bridge] Discord -> Telegram: Channel {s}, User {s} ({s}): {s}\n", .{ channel_id, username, user_id, message_text });
        
        // Skip empty messages
        if (message_text.len == 0) {
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
        print("\n=== Starting Zefxi Bridge ===\n", .{});
        print("Telegram Chat ID: {?d}\n", .{self.config.telegram_chat_id});
        print("Discord Channel ID: {s}\n", .{self.config.discord_channel_id});
        print("Debug mode: {}\n", .{self.config.debug_mode});
        print("Press Ctrl+C to exit\n\n", .{});

        // Set up message handlers
        self.telegram_client.setMessageHandler(self, &onTelegramMessage);
        self.discord_client.setMessageHandler(self, &onDiscordMessage);

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

    fn startDiscordClient(client: *discord.DiscordClient) void {
        client.start() catch |err| {
            print("[Bridge] Discord client error: {}\n", .{err});
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

    // Get debug mode
    var debug_mode = false;
    if (std.process.getEnvVarOwned(allocator, "DEBUG")) |debug_str| {
        defer allocator.free(debug_str);
        debug_mode = std.mem.eql(u8, debug_str, "1") or std.mem.eql(u8, debug_str, "true");
    } else |_| {
        // Default to false
    }

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
    };

    var bridge = Bridge.init(allocator, config) catch |err| {
        print("Failed to initialize bridge: {}\n", .{err});
        return;
    };
    defer bridge.deinit();

    try bridge.start();

    print("\nBridge session completed!\n", .{});
} 