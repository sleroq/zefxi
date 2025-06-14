const std = @import("std");
const log = std.log.scoped(.telegram_client);
const telegram = @import("telegram.zig");
const helpers = @import("../helpers.zig");
const errors = @import("../errors.zig");
const messages = @import("messages.zig");

pub const TelegramEvent = union(enum) {
    new_message: NewMessageEvent,
    edited_message: EditedMessageEvent,
    user_update: UserUpdateEvent,
    file_update: FileUpdateEvent,
    new_chat: NewChatEvent,
    auth_update: AuthUpdateEvent,
    unknown: UnknownEvent,

    pub fn deinit(self: *TelegramEvent, allocator: std.mem.Allocator) void {
        switch (self.*) {
            .new_message => |*event| event.deinit(allocator),
            .edited_message => |*event| event.deinit(allocator),
            .user_update => |*event| event.deinit(allocator),
            .file_update => |*event| event.deinit(allocator),
            .new_chat => |*event| event.deinit(allocator),
            .auth_update => |*event| event.deinit(allocator),
            .unknown => |*event| event.deinit(allocator),
        }
    }
};

pub const NewMessageEvent = struct {
    chat_id: i64,
    message_id: i64,
    sender_id: i64,
    date: i64,
    content: telegram.MessageContent,
    is_outgoing: bool,
    is_pinned: bool,

    pub fn deinit(self: *NewMessageEvent, allocator: std.mem.Allocator) void {
        switch (self.content) {
            .text => {},
            .attachment => |*attachment| attachment.deinit(allocator),
            .text_with_attachment => |*text_attachment| text_attachment.attachment.deinit(allocator),
        }
    }
};

pub const EditedMessageEvent = struct {
    chat_id: i64,
    message_id: i64,
    edit_date: i64,
    content: telegram.MessageContent,

    pub fn deinit(self: *EditedMessageEvent, allocator: std.mem.Allocator) void {
        switch (self.content) {
            .text => {},
            .attachment => |*attachment| attachment.deinit(allocator),
            .text_with_attachment => |*text_attachment| text_attachment.attachment.deinit(allocator),
        }
    }
};

pub const UserUpdateEvent = struct {
    user_id: i64,
    first_name: []const u8,
    last_name: ?[]const u8,
    username: ?[]const u8,
    avatar_url: ?[]const u8,

    pub fn deinit(self: *UserUpdateEvent, allocator: std.mem.Allocator) void {
        allocator.free(self.first_name);
        if (self.last_name) |name| allocator.free(name);
        if (self.username) |name| allocator.free(name);
        if (self.avatar_url) |url| allocator.free(url);
    }
};

pub const FileUpdateEvent = struct {
    file_id: i64,
    size: i64,
    local_path: ?[]const u8,
    is_downloading_completed: bool,
    is_downloading_active: bool,
    download_offset: i64,
    downloaded_size: i64,

    pub fn deinit(self: *FileUpdateEvent, allocator: std.mem.Allocator) void {
        if (self.local_path) |path| allocator.free(path);
    }
};

pub const NewChatEvent = struct {
    chat_id: i64,
    title: []const u8,
    type: ChatType,
    member_count: i64,

    pub fn deinit(self: *NewChatEvent, allocator: std.mem.Allocator) void {
        allocator.free(self.title);
    }
};

pub const WaitCode = struct {
    code_info: ?CodeInfo,
};

pub const CodeInfo = struct {
    type: ?[]const u8,
    length: ?i32,
    next_type: ?[]const u8,
};

pub const WaitPassword = struct {
    password_hint: ?[]const u8,
    has_recovery_email_address: ?bool,
    recovery_email_address_pattern: ?[]const u8,
};

/// Represents all possible Telegram authorization states.
pub const AuthState = union(enum) {
    wait_tdlib_parameters,
    wait_encryption_key,
    wait_phone_number,
    wait_code: WaitCode,
    wait_password: WaitPassword,
    ready,
    logging_out,
    closing,
    closed,
    unknown: []const u8, // fallback for unknown state types
};

/// AuthUpdateEvent represents a change in the Telegram authorization state.
pub const AuthUpdateEvent = struct {
    state: AuthState,
    raw: []const u8,

    /// Deinit frees all heap-allocated fields.
    pub fn deinit(self: *AuthUpdateEvent, allocator: std.mem.Allocator) void {
        switch (self.state) {
            .unknown => |s| allocator.free(s),
            else => {},
        }
        allocator.free(self.raw);
    }
};

pub const UnknownEvent = struct {
    type: []const u8,
    raw: []const u8,

    pub fn deinit(self: *UnknownEvent, allocator: std.mem.Allocator) void {
        allocator.free(self.type);
        allocator.free(self.raw);
    }
};

pub const ChatType = enum {
    private,
    group,
    supergroup,
    channel,
};

pub fn parseTelegramEvent(
    allocator: std.mem.Allocator,
    update_json: []const u8,
) !TelegramEvent {
    const parsed = try std.json.parseFromSliceLeaky(std.json.Value, allocator, update_json, .{});

    const root = parsed.value.object;

    const update_type = root.get("@type") orelse return error.MissingUpdateType;
    const type_str = update_type.string;

    if (std.mem.eql(u8, type_str, "updateAuthorizationState")) {
        return try parseAuthUpdateEvent(allocator, root, update_json);
    } else if (std.mem.eql(u8, type_str, "updateNewMessage")) {
        return parseNewMessageEvent(allocator, root);
    }
    // } else if (std.mem.eql(u8, type_str, "updateMessageEdited")) {
    //     return parseEditedMessageEvent(allocator, root);
    // } else if (std.mem.eql(u8, type_str, "updateUser")) {
    //     return parseUserUpdateEvent(allocator, root);
    // } else if (std.mem.eql(u8, type_str, "updateFile")) {
    //     return parseFileUpdateEvent(allocator, root);
    // } else if (std.mem.eql(u8, type_str, "updateNewChat")) {
    //     return parseNewChatEvent(allocator, root);
    // }

    // TODO: Think with my brain
    // For unknown update types, just log and return the type
    log.warn("Unknown Telegram update type: {s}", .{type_str});
    return TelegramEvent{ .unknown = .{
        .type = try allocator.dupe(u8, type_str),
        .raw = "",
    } };
}

fn parseAuthUpdateEvent(allocator: std.mem.Allocator, root: std.json.ObjectMap, update_json: []const u8) !TelegramEvent {
    var authState = helpers.jsonGetString(root, "authorization_state")


    if (root.get("authorization_state")) |state_val| {
        if (state_val.object.get("@type")) |state_type_val| {

            const state_type = helpers.jsonGetString(allocator, key: []const u8, allocator: Allocator)state_type_val.string;

            const state = if (std.mem.eql(u8, state_type, "authorizationStateWaitTdlibParameters")) AuthState.wait_tdlib_parameters else if (std.mem.eql(u8, state_type, "authorizationStateWaitEncryptionKey")) AuthState.wait_encryption_key else if (std.mem.eql(u8, state_type, "authorizationStateWaitPhoneNumber")) AuthState.wait_phone_number else if (std.mem.eql(u8, state_type, "authorizationStateWaitCode")) blk: {
                var code_info: ?CodeInfo = null;
                if (state_val.object.get("code_info")) |ci| {
                    const ci_obj = ci.object;
                    code_info = CodeInfo{
                        .type = if (ci_obj.get("type")) |t| t.string else null,
                        .length = if (ci_obj.get("length")) |l|
                            if (l.integer >= @as(i64, std.math.minInt(i32)) and l.integer <= @as(i64, std.math.maxInt(i32)))
                                @as(i32, @truncate(l.integer))
                            else
                                null
                        else
                            null,
                        .next_type = if (ci_obj.get("next_type")) |nt| nt.string else null,
                    };
                }
                break :blk AuthState{ .wait_code = WaitCode{ .code_info = code_info } };
            } else if (std.mem.eql(u8, state_type, "authorizationStateWaitPassword")) blk: {
                var password_hint: ?[]const u8 = null;
                var has_recovery_email_address: ?bool = null;
                var recovery_email_address_pattern: ?[]const u8 = null;
                if (state_val.object.get("password_hint")) |ph| password_hint = ph.string;
                if (state_val.object.get("has_recovery_email_address")) |hrea| has_recovery_email_address = hrea.bool;
                if (state_val.object.get("recovery_email_address_pattern")) |reap| recovery_email_address_pattern = reap.string;
                break :blk AuthState{ .wait_password = WaitPassword{
                    .password_hint = password_hint,
                    .has_recovery_email_address = has_recovery_email_address,
                    .recovery_email_address_pattern = recovery_email_address_pattern,
                } };
            } else if (std.mem.eql(u8, state_type, "authorizationStateReady")) AuthState.ready else if (std.mem.eql(u8, state_type, "authorizationStateLoggingOut")) AuthState.logging_out else if (std.mem.eql(u8, state_type, "authorizationStateClosing")) AuthState.closing else if (std.mem.eql(u8, state_type, "authorizationStateClosed")) AuthState.closed else AuthState{ .unknown = try allocator.dupe(u8, state_type) };
            return TelegramEvent{ .auth_update = .{
                .state = state,
                .raw = try allocator.dupe(u8, update_json),
            } };
        }
    }
    // fallback if structure is unexpected
    return TelegramEvent{ .auth_update = .{
        .state = AuthState{ .unknown = try allocator.dupe(u8, "unknown") },
        .raw = try allocator.dupe(u8, update_json),
    } };
}

fn parseNewMessageEvent(
    allocator: std.mem.Allocator,
    root: std.json.ObjectMap,
) !TelegramEvent {
    log.info("Parsing new message event", .{});
    const message = root.get("message") orelse return error.MissingMessage;
    const msg_obj = message.object;

    const chat_id = try helpers.jsonGetI64(msg_obj, "chat_id", allocator);
    const message_id = try helpers.jsonGetI64(msg_obj, "id", allocator);
    const date = try helpers.jsonGetI64(msg_obj, "date", allocator);
    const is_outgoing = try helpers.jsonGetBool(msg_obj, "is_outgoing", allocator);
    const is_pinned = try helpers.jsonGetBool(msg_obj, "is_pinned", allocator);

    var sender_id: i64 = 0;
    if (msg_obj.get("sender_id")) |sender| {
        const sender_obj = sender.object;
        if (sender_obj.get("user_id")) |user_id| {
            sender_id = user_id.integer;
        }
    }

    // I am unsure about the types
    var content: telegram.MessageContent = .{ .text = "" };
    if (msg_obj.get("content")) |content_val| {
        const content_obj = content_val.object;
        if (content_obj.get("@type")) |content_type| {
            const type_str = content_type.string;
            if (std.mem.eql(u8, type_str, "messageText")) {
                if (content_obj.get("text")) |text| {
                    const text_obj = text.object;
                    if (text_obj.get("text")) |text_content| {
                        content = telegram.MessageContent{ .text = text_content.string };
                    }
                }
            // } else if (std.mem.eql(u8, type_str, "messagePhoto")) {
            //     content = try messages.parsePhotoMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageDocument")) {
            //     content = try messages.parseDocumentMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVideo")) {
            //     content = try messages.parseVideoMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageAudio")) {
            //     content = try messages.parseAudioMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVoiceNote")) {
            //     content = try messages.parseVoiceMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVideoNote")) {
            //     content = try messages.parseVideoNoteMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageSticker")) {
            //     content = try messages.parseStickerMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageAnimation")) {
            //     content = try messages.parseAnimationMessage(allocator, content_obj, chat_id, sender_id) orelse .{ .text = "" };
            } else {
                content = .{ .text = "" };
            }
        } else {
            content = .{ .text = "" };
        }
    }

    return TelegramEvent{ .new_message = .{
        .chat_id = chat_id,
        .message_id = message_id,
        .sender_id = sender_id,
        .date = date,
        .content = content,
        .is_outgoing = is_outgoing,
        .is_pinned = is_pinned,
    } };
}

fn parseEditedMessageEvent(
    allocator: std.mem.Allocator,
    root: std.json.ObjectMap,
) !TelegramEvent {
    const message = root.get("message") orelse return error.MissingMessage;
    const msg_obj = message.object;

    const chat_id = try helpers.jsonGetI64(msg_obj, "chat_id", allocator);
    const message_id = try helpers.jsonGetI64(msg_obj, "id", allocator);
    const edit_date = try helpers.jsonGetI64(msg_obj, "edit_date", allocator);

    var content: telegram.MessageContent = .{ .text = "" };
    if (msg_obj.get("content")) |content_val| {
        const content_obj = content_val.object;
        if (content_obj.get("@type")) |content_type| {
            const type_str = content_type.string;
            if (std.mem.eql(u8, type_str, "messageText")) {
                if (content_obj.get("text")) |text| {
                    const text_obj = text.object;
                    if (text_obj.get("text")) |text_content| {
                        content = telegram.MessageContent{ .text = text_content.string };
                    }
                }
            // else if (std.mem.eql(u8, type_str, "messagePhoto")) {
            //     content = try messages.parsePhotoMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageDocument")) {
            //     content = try messages.parseDocumentMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVideo")) {
            //     content = try messages.parseVideoMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageAudio")) {
            //     content = try messages.parseAudioMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVoiceNote")) {
            //     content = try messages.parseVoiceMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageVideoNote")) {
            //     content = try messages.parseVideoNoteMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageSticker")) {
            //     content = try messages.parseStickerMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            // } else if (std.mem.eql(u8, type_str, "messageAnimation")) {
            //     content = try messages.parseAnimationMessage(allocator, content_obj, chat_id, 0, startFileDownload) orelse .{ .text = "" };
            } else {
                content = .{ .text = "" };
            }
        } else {
            content = .{ .text = "" };
        }
    }

    return TelegramEvent{ .edited_message = .{
        .chat_id = chat_id,
        .message_id = message_id,
        .edit_date = edit_date,
        .content = content,
    } };
}

fn parseUserUpdateEvent(allocator: std.mem.Allocator, root: std.json.ObjectMap) !TelegramEvent {
    const user = root.get("user") orelse return error.MissingUser;
    const user_obj = user.object;

    const user_id = try helpers.jsonGetI64(user_obj, "id", allocator);
    const first_name = try helpers.jsonGetString(user_obj, "first_name", allocator);
    const last_name = try helpers.jsonGetStringOptional(user_obj, "last_name", allocator);
    const username = try helpers.jsonGetStringOptional(user_obj, "username", allocator);

    return TelegramEvent{ .user_update = .{
        .user_id = user_id,
        .first_name = first_name,
        .last_name = last_name,
        .username = username,
        .avatar_url = null,
    } };
}

fn parseFileUpdateEvent(allocator: std.mem.Allocator, root: std.json.ObjectMap) !TelegramEvent {
    const file = root.get("file") orelse return error.MissingFile;
    const file_obj = file.object;

    const file_id = try helpers.jsonGetI64(file_obj, "id", allocator);
    const size = try helpers.jsonGetI64(file_obj, "size", allocator);

    var local_path: ?[]const u8 = null;
    var is_downloading_completed = false;
    var is_downloading_active = false;
    var download_offset: i64 = 0;
    var downloaded_size: i64 = 0;

    if (file_obj.get("local")) |local| {
        const local_obj = local.object;
        if (local_obj.get("path")) |path| {
            local_path = try allocator.dupe(u8, path.string);
        }
        if (local_obj.get("is_downloading_completed")) |completed| {
            is_downloading_completed = completed.bool;
        }
        if (local_obj.get("is_downloading_active")) |active| {
            is_downloading_active = active.bool;
        }
        if (local_obj.get("download_offset")) |offset| {
            download_offset = offset.integer;
        }
        if (local_obj.get("downloaded_size")) |downloaded| {
            downloaded_size = downloaded.integer;
        }
    }

    return TelegramEvent{ .file_update = .{
        .file_id = file_id,
        .size = size,
        .local_path = local_path,
        .is_downloading_completed = is_downloading_completed,
        .is_downloading_active = is_downloading_active,
        .download_offset = download_offset,
        .downloaded_size = downloaded_size,
    } };
}

fn parseNewChatEvent(allocator: std.mem.Allocator, root: std.json.ObjectMap) !TelegramEvent {
    const chat = root.get("chat") orelse return error.MissingChat;
    const chat_obj = chat.object;

    const chat_id = try helpers.jsonGetI64(chat_obj, "id", allocator);
    const title = try helpers.jsonGetString(chat_obj, "title", allocator);
    const type_str = try helpers.jsonGetString(chat_obj, "type", allocator);
    const member_count = try helpers.jsonGetI64(chat_obj, "member_count", allocator);

    const chat_type = if (std.mem.eql(u8, type_str, "private"))
        ChatType.private
    else if (std.mem.eql(u8, type_str, "group"))
        ChatType.group
    else if (std.mem.eql(u8, type_str, "supergroup"))
        ChatType.supergroup
    else if (std.mem.eql(u8, type_str, "channel"))
        ChatType.channel
    else
        return error.UnknownChatType;

    return TelegramEvent{ .new_chat = .{
        .chat_id = chat_id,
        .title = title,
        .type = chat_type,
        .member_count = member_count,
    } };
}
