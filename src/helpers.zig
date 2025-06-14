const std = @import("std");
const log = std.log.scoped(.helpers);
const Allocator = std.mem.Allocator;

pub const JsonHelperError = error{
    MissingField,
    InvalidType,
    OutOfMemory,
};

pub fn jsonGetString(obj: std.json.ObjectMap, key: []const u8) error{ MissingField, InvalidType, OutOfMemory }![]const u8 {
    const value = obj.get(key) orelse return error.MissingField;

    switch (value) {
        .string => |str| return str,
        else => return error.InvalidType,
    }
}

pub fn jsonGetStringOptional(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) error{ InvalidType, OutOfMemory }!?[]const u8 {
    if (obj.get(key)) |value| {
        switch (value) {
            .string => |str| return try allocator.dupe(u8, str),
            .null => return null,
            else => return error.InvalidType,
        }
    }
    return null;
}

pub fn jsonGetI64(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError!i64 {
    _ = allocator;
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .integer => |i| return i,
        else => return error.InvalidType,
    }
}

pub fn jsonGetBool(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError!bool {
    _ = allocator;
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .bool => |b| return b,
        else => return error.InvalidType,
    }
}

pub fn jsonGetI32(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError!i32 {
    _ = allocator;
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .integer => |i| return @intCast(i),
        else => return error.InvalidType,
    }
}

pub fn jsonGetF64(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError!f64 {
    _ = allocator;
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .float => |f| return f,
        .integer => |i| return @floatFromInt(i),
        else => return error.InvalidType,
    }
}

pub fn jsonGetArray(obj: std.json.ObjectMap, key: []const u8) JsonHelperError!std.json.Array {
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .array => |arr| return arr,
        else => return error.InvalidType,
    }
}

pub fn jsonGetObject(obj: std.json.ObjectMap, key: []const u8) JsonHelperError!std.json.ObjectMap {
    const value = obj.get(key) orelse return error.MissingField;
    switch (value) {
        .object => |o| return o,
        else => return error.InvalidType,
    }
}

pub fn jsonGetStringArray(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError![][]const u8 {
    const array = try jsonGetArray(obj, key);
    var result = try std.ArrayList([]const u8).initCapacity(allocator, array.items.len);
    errdefer {
        for (result.items) |item| {
            allocator.free(item);
        }
        result.deinit();
    }

    for (array.items) |item| {
        switch (item) {
            .string => |str| try result.append(try allocator.dupe(u8, str)),
            else => return error.InvalidType,
        }
    }

    return result.toOwnedSlice();
}

pub fn jsonGetI64Array(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError![]i64 {
    const array = try jsonGetArray(obj, key);
    var result = try allocator.alloc(i64, array.items.len);
    errdefer allocator.free(result);

    for (array.items, 0..) |item, i| {
        switch (item) {
            .integer => |int| result[i] = int,
            else => return error.InvalidType,
        }
    }

    return result;
}

pub fn jsonGetBoolArray(obj: std.json.ObjectMap, key: []const u8, allocator: std.mem.Allocator) JsonHelperError![]bool {
    const array = try jsonGetArray(obj, key);
    var result = try allocator.alloc(bool, array.items.len);
    errdefer allocator.free(result);

    for (array.items, 0..) |item, i| {
        switch (item) {
            .bool => |b| result[i] = b,
            else => return error.InvalidType,
        }
    }

    return result;
}

pub fn jsonGetObjectArray(obj: std.json.ObjectMap, key: []const u8) JsonHelperError![]std.json.ObjectMap {
    const array = try jsonGetArray(obj, key);
    var result = try std.ArrayList(std.json.ObjectMap).initCapacity(array.items.len);
    errdefer result.deinit();

    for (array.items) |item| {
        switch (item) {
            .object => |o| try result.append(o),
            else => return error.InvalidType,
        }
    }

    return result.toOwnedSlice();
}

// TODO: This is not ideal and not tested solution
pub fn escapeDiscordMarkdown(allocator: Allocator, text: []const u8) ![]const u8 {
    var result = std.ArrayList(u8).init(allocator);
    defer result.deinit();

    var i: usize = 0;
    while (i < text.len) {
        const char = text[i];
        switch (char) {
            // Discord markdown special characters
            '*', '_', '`', '~', '\\' => {
                try result.append('\\');
                try result.append(char);
            },
            '|' => {
                // Check for spoiler tags ||
                if (i + 1 < text.len and text[i + 1] == '|') {
                    try result.append('\\');
                    try result.append('|');
                    try result.append('\\');
                    try result.append('|');
                    i += 1; // Skip the next |
                } else {
                    try result.append(char);
                }
            },
            '#' => {
                // Only escape # at the beginning of a line (headers)
                if (i == 0 or text[i - 1] == '\n') {
                    try result.append('\\');
                }
                try result.append(char);
            },
            '>' => {
                // Only escape > at the beginning of a line (block quotes)
                if (i == 0 or text[i - 1] == '\n') {
                    try result.append('\\');
                }
                try result.append(char);
            },
            '-' => {
                // Escape - at the beginning of a line (lists and small text)
                if (i == 0 or text[i - 1] == '\n') {
                    try result.append('\\');
                }
                try result.append(char);
            },
            '[', ']', '(', ')' => {
                // Escape link markdown
                try result.append('\\');
                try result.append(char);
            },
            else => {
                // Check for numbered lists (number followed by .)
                if (char >= '0' and char <= '9') {
                    // Look ahead to see if this is a numbered list
                    var j = i + 1;
                    while (j < text.len and text[j] >= '0' and text[j] <= '9') {
                        j += 1;
                    }
                    if (j < text.len and text[j] == '.' and (i == 0 or text[i - 1] == '\n')) {
                        // This is a numbered list, escape the dot
                        while (i < j) {
                            try result.append(text[i]);
                            i += 1;
                        }
                        try result.append('\\');
                        try result.append('.');
                        // Skip the dot since we just added it
                        // Don't increment i here as it will be done at the end of the loop
                    } else {
                        try result.append(char);
                    }
                } else {
                    try result.append(char);
                }
            },
        }
        i += 1;
    }

    return result.toOwnedSlice();
}

pub fn escapeTelegramMarkdown(allocator: Allocator, text: []const u8) ![]const u8 {
    var result = std.ArrayList(u8).init(allocator);
    defer result.deinit();

    for (text) |char| {
        switch (char) {
            // Telegram MarkdownV2 special characters that must be escaped
            '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!', '\\' => {
                try result.append('\\');
                try result.append(char);
            },
            else => try result.append(char),
        }
    }

    return result.toOwnedSlice();
}
