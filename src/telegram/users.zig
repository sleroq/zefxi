// Telegram TDLib User Info and Cache
const std = @import("std");
const Allocator = std.mem.Allocator;

/// UserInfo owns all its string fields and must be deinitialized with deinit().
pub const UserInfo = struct {
    user_id: i64,
    first_name: []const u8,
    last_name: ?[]const u8,
    username: ?[]const u8,
    avatar_url: ?[]const u8,

    /// Returns a display name. Caller owns the returned memory.
    pub fn getDisplayName(self: UserInfo, allocator: Allocator) ![]u8 {
        if (self.last_name) |lname| {
            return std.fmt.allocPrint(allocator, "{s} {s}", .{ self.first_name, lname });
        } else {
            return std.fmt.allocPrint(allocator, "{s}", .{self.first_name});
        }
    }

    /// Frees all heap-allocated fields.
    pub fn deinit(self: *UserInfo, allocator: Allocator) void {
        allocator.free(self.first_name);
        if (self.last_name) |lname| allocator.free(lname);
        if (self.username) |uname| allocator.free(uname);
        if (self.avatar_url) |url| allocator.free(url);
    }
};
// (Add user cache logic here if needed)
