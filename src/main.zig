const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;

const TelegramBot = struct {
    token: []const u8,
    allocator: Allocator,
    client: std.http.Client,

    const Self = @This();

    pub fn init(allocator: Allocator, token: []const u8) Self {
        return Self{
            .token = token,
            .allocator = allocator,
            .client = std.http.Client{ .allocator = allocator },
        };
    }

    pub fn deinit(self: *Self) void {
        self.client.deinit();
    }

    pub fn getMe(self: *Self) !void {
        const url = try std.fmt.allocPrint(self.allocator, "https://api.telegram.org/bot{s}/getMe", .{self.token});
        defer self.allocator.free(url);

        const uri = try std.Uri.parse(url);
        
        var request = try self.client.open(.GET, uri, .{
            .server_header_buffer = try self.allocator.alloc(u8, 4096),
        });
        defer request.deinit();

        try request.send();
        try request.finish();
        try request.wait();

        const response_body = try request.reader().readAllAlloc(self.allocator, 1024 * 1024);
        defer self.allocator.free(response_body);

        print("Telegram API Response:\n{s}\n", .{response_body});
    }

    pub fn getUpdates(self: *Self) !void {
        const url = try std.fmt.allocPrint(self.allocator, "https://api.telegram.org/bot{s}/getUpdates", .{self.token});
        defer self.allocator.free(url);

        const uri = try std.Uri.parse(url);
        
        var request = try self.client.open(.GET, uri, .{
            .server_header_buffer = try self.allocator.alloc(u8, 4096),
        });
        defer request.deinit();

        try request.send();
        try request.finish();
        try request.wait();

        const response_body = try request.reader().readAllAlloc(self.allocator, 1024 * 1024);
        defer self.allocator.free(response_body);

        print("Telegram Updates:\n{s}\n", .{response_body});
    }
};

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // Get token from environment variable
    const token = std.process.getEnvVarOwned(allocator, "TELEGRAM_BOT_TOKEN") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_BOT_TOKEN environment variable\n", .{});
            print("Usage: TELEGRAM_BOT_TOKEN=your_bot_token zig run src/main.zig\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(token);

    print("Starting Zefxi - Telegram/Discord Bridge Server\n", .{});
    print("Using bot token: {s}...\n", .{token[0..@min(10, token.len)]});

    var bot = TelegramBot.init(allocator, token);
    defer bot.deinit();

    print("\n=== Testing Telegram API Connection ===\n", .{});
    
    // Test getMe endpoint
    bot.getMe() catch |err| {
        print("Error getting bot info: {}\n", .{err});
        return;
    };

    print("\n=== Getting Recent Updates ===\n", .{});
    
    // Test getUpdates endpoint
    bot.getUpdates() catch |err| {
        print("Error getting updates: {}\n", .{err});
        return;
    };

    print("\n=== API Tests Complete ===\n", .{});
} 