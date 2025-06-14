const std = @import("std");

pub const Error = struct {
    code: []const u8,
    message: []const u8,
    allocator: std.mem.Allocator,

    pub fn deinit(self: *Error) void {
        self.allocator.free(self.code);
        self.allocator.free(self.message);
    }
};

pub fn Result(comptime T: type) type {
    return union(enum) {
        ok: T,
        err: Error,

        pub fn deinit(self: *Result(T)) void {
            switch (self.*) {
                .ok => {},
                .err => |*e| e.deinit(),
            }
        }
    };
}

pub fn ok(comptime T: type, value: T) Result(T) {
    return Result(T){ .ok = value };
}

pub fn err(comptime T: type, allocator: std.mem.Allocator, code: []const u8, message: []const u8) Result(T) {
    return Result(T){ .err = Error{
        .code = allocator.dupe(u8, code) catch unreachable,
        .message = allocator.dupe(u8, message) catch unreachable,
        .allocator = allocator,
    } };
}

pub fn makeError(allocator: std.mem.Allocator, code: []const u8, message: []const u8) Error {
    return Error{
        .code = allocator.dupe(u8, code) catch unreachable,
        .message = allocator.dupe(u8, message) catch unreachable,
        .allocator = allocator,
    };
}
