const std = @import("std");
const log = std.log.scoped(.telegram);
const Allocator = std.mem.Allocator;
const c = std.c;
const helpers = @import("../helpers.zig");
const errors = @import("../errors.zig");
const telegram_event = @import("telegram_event.zig");
const client_mod = @import("client.zig");
const users_mod = @import("users.zig");
const files_mod = @import("files.zig");
const messages_mod = @import("messages.zig");

extern "c" fn td_create_client_id() c_int;
extern "c" fn td_send(client_id: c_int, request: [*:0]const u8) void;
extern "c" fn td_receive(timeout: f64) ?[*:0]const u8;
extern "c" fn td_execute(request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_set_log_verbosity_level(level: c_int) void;

pub const Config = struct {
    database_directory: []const u8 = "tdlib",
    files_directory: []const u8 = "tdlib",
    system_language_code: []const u8 = "en",
    device_model: []const u8 = "Desktop",
    system_version: []const u8 = "Linux",
    application_version: []const u8 = "1.0",
    log_verbosity: i32 = 2,
    receive_timeout: f64 = 1.0,
    use_test_dc: bool = false,
    enable_storage_optimizer: bool = true,
    debug_mode: bool = false,
};

pub const TelegramError = error{
    ClientNotInitialized,
    ClientClosed,
    JsonParseError,
    InvalidId,
    InvalidFileId,
    InvalidDimensions,
    MissingAttachmentData,
    MalformedUpdate,
};

pub const UserInfo = users_mod.UserInfo;
pub const AttachmentInfo = files_mod.AttachmentInfo;
pub const AttachmentType = files_mod.AttachmentType;
pub const MessageContent = messages_mod.MessageContent;

pub const MessageHandler = *const fn (ctx: *anyopaque, chat_id: i64, user_info: UserInfo, content: MessageContent) void;

pub const CallbackError = error{
    RequestFailed,
    InvalidResponse,
    CallbackNotFound,
};

pub const Callback = *const fn (ctx: ?*anyopaque, result: CallbackResult) void;

pub const CallbackResult = union(enum) {
    success: std.json.Value,
    error_response: struct {
        code: i32,
        message: []const u8,
    },
    pub fn deinit(self: *CallbackResult, allocator: Allocator) void {
        switch (self.*) {
            .success => |*value| {
                _ = value;
            },
            .error_response => |*err| {
                allocator.free(err.message);
            },
        }
    }
};

pub const TelegramClient = client_mod.TelegramClient;
