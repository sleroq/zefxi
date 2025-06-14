const std = @import("std");
const Allocator = std.mem.Allocator;
const Discord = @import("discord");

const log = std.log.scoped(.config);

pub const BridgeConfig = struct {
    debug_mode: bool = false,
    telegram_api_id: i32,
    telegram_api_hash: []const u8,
    telegram_chat_id: i64,
    discord_token: []const u8,
    discord_server_id: Discord.Snowflake,
    discord_channel_id: Discord.Snowflake,
    discord_webhook_url: []const u8,
    file_server_port: u16 = 8080,
    files_directory: []const u8 = "attachments",

    pub fn deinit(self: *const BridgeConfig, allocator: Allocator) void {
        allocator.free(self.telegram_api_hash);
        allocator.free(self.discord_token);
        allocator.free(self.discord_webhook_url);
    }
};

fn parseRequiredEnvSnowflake(allocator: Allocator, env_var: []const u8) !Discord.Snowflake {
    const value = std.process.getEnvVarOwned(allocator, env_var) catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            log.err("Please set {s} environment variable", .{env_var});
            return err;
        },
        error.OutOfMemory => {
            log.err("Out of memory while reading {s}", .{env_var});
            return err;
        },
        error.InvalidWtf8 => {
            log.err("{s} contains invalid WTF-7", .{env_var});
            return err;
        },
    };
    defer allocator.free(value);

    return Discord.Snowflake.fromRaw(value) catch |err| {
        log.err("{s} must be a valid Discord snowflake (got: '{s}'): {}", .{ env_var, value, err });
        return err;
    };
}

fn parseRequiredEnvI64(allocator: Allocator, env_var: []const u8) !i64 {
    const value = std.process.getEnvVarOwned(allocator, env_var) catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            log.err("Please set {s} environment variable", .{env_var});
            return err;
        },
        error.OutOfMemory => {
            log.err("Out of memory while reading {s}", .{env_var});
            return err;
        },
        error.InvalidWtf8 => {
            log.err("{s} contains invalid WTF-7", .{env_var});
            return err;
        },
    };
    defer allocator.free(value);

    return std.fmt.parseInt(i64, value, 10) catch |err| {
        log.err("{s} must be a valid integer (got: '{s}'): {}", .{ env_var, value, err });
        return err;
    };
}

fn parseDebugMode(allocator: Allocator) bool {
    const debug_str = std.process.getEnvVarOwned(allocator, "DEBUG") catch return false;
    defer allocator.free(debug_str);

    return std.mem.eql(u8, debug_str, "1") or
        std.mem.eql(u8, debug_str, "true") or
        std.mem.eql(u8, debug_str, "TRUE");
}

fn getRequiredEnvVar(allocator: Allocator, env_var: []const u8, description: []const u8) ![]const u8 {
    return std.process.getEnvVarOwned(allocator, env_var) catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            log.err("Please set {s} environment variable", .{env_var});
            log.info("{s}", .{description});
            return err;
        },
        else => return err,
    };
}

pub fn parseConfig(allocator: Allocator) !BridgeConfig {
    const api_id_str = try getRequiredEnvVar(allocator, "TELEGRAM_API_ID", "Get your API credentials from https://my.telegram.org/apps");
    const api_hash = try getRequiredEnvVar(allocator, "TELEGRAM_API_HASH", "Get your API credentials from https://my.telegram.org/apps");
    const discord_token = try getRequiredEnvVar(allocator, "DISCORD_TOKEN", "Get your bot token from https://discord.com/developers/applications");
    const discord_webhook_url = try getRequiredEnvVar(allocator, "DISCORD_WEBHOOK_URL", "Create a webhook in your Discord channel settings");

    const api_id = std.fmt.parseInt(i32, api_id_str, 10) catch |err| {
        log.err("TELEGRAM_API_ID must be a valid integer", .{});
        return err;
    };

    const telegram_chat_id = try parseRequiredEnvI64(allocator, "TELEGRAM_CHAT_ID");
    const discord_server_id = try parseRequiredEnvSnowflake(allocator, "DISCORD_SERVER");
    const discord_channel_id = try parseRequiredEnvSnowflake(allocator, "DISCORD_CHANNEL");

    const debug_mode = parseDebugMode(allocator);

    // const attachments_base_url = std.process.getEnvVarOwned(allocator, "ATTACHMENTS_BASE_URL") catch |err| switch (err) {
    //     error.EnvironmentVariableNotFound => try allocator.dupe(u8, "http://127.0.0.1:8080"),
    //     else => return err,
    // };

    return BridgeConfig{
        .debug_mode = debug_mode,
        .telegram_api_id = api_id,
        .telegram_api_hash = api_hash,
        .telegram_chat_id = telegram_chat_id,
        .discord_token = discord_token,
        .discord_server_id = discord_server_id,
        .discord_channel_id = discord_channel_id,
        .discord_webhook_url = discord_webhook_url,
    };
}
