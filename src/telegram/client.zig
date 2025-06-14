const std = @import("std");
const log = std.log.scoped(.telegram_client);
const Allocator = std.mem.Allocator;
const c = std.c;
const helpers = @import("../helpers.zig");
const telegram_event = @import("telegram_event.zig");

extern "c" fn td_create_client_id() c_int;
extern "c" fn td_send(client_id: c_int, request: [*:0]const u8) void;
extern "c" fn td_receive(timeout: f64) ?[*:0]const u8;
extern "c" fn td_execute(request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_set_log_verbosity_level(level: c_int) void;

const Config = @import("telegram.zig").Config;
const UserInfo = @import("telegram.zig").UserInfo;
const MessageHandler = @import("telegram.zig").MessageHandler;
const Callback = @import("telegram.zig").Callback;
const CallbackResult = @import("telegram.zig").CallbackResult;
const TelegramError = @import("telegram.zig").TelegramError;

const PendingCallback = struct {
    callback: Callback,
    context: ?*anyopaque,
    request_id: []const u8,
    allocator: Allocator,
    fn deinit(self: *PendingCallback) void {
        self.allocator.free(self.request_id);
    }
};

const EventType = enum {
    new_message,
    edited_message,
    user_update,
    file_update,
    new_chat,
    auth_update,
    unknown,
};

const HandlerFn = *const fn (ctx: ?*anyopaque, event: *const telegram_event.TelegramEvent) void;

const HandlerEntry = struct {
    handler: HandlerFn,
    ctx: ?*anyopaque,
};

pub const TelegramClient = struct {
    allocator: Allocator,

    api_id: i32,
    api_hash: []const u8,
    client_id: c_int,
    is_authorized: bool,
    is_closed: bool,

    config: Config,

    // event_handlers: EventHandlers,
    // send_buffer: std.ArrayList(u8),
    my_user_info: ?UserInfo = null,
    // user_cache: std.HashMap(i64, UserInfo, std.hash_map.AutoContext(i64), std.hash_map.default_max_load_percentage),
    pending_callbacks: std.HashMap([]const u8, PendingCallback, std.hash_map.StringContext, std.hash_map.default_max_load_percentage),
    // request_counter: std.atomic.Value(u64),

    // const Self = @This();
    //
    // fn buildJsonRequest(self: *Self, request_type: []const u8, params: anytype) ![]u8 {
    //     var json_map = std.json.ObjectMap.init(self.allocator);
    //     defer json_map.deinit();
    //
    //     try json_map.put("@type", std.json.Value{ .string = request_type });
    //
    //     inline for (std.meta.fields(@TypeOf(params))) |field| {
    //         const value = @field(params, field.name);
    //         const json_value = switch (@TypeOf(value)) {
    //             []const u8, []u8 => std.json.Value{ .string = value },
    //             i32, i64 => std.json.Value{ .integer = @intCast(value) },
    //             bool => std.json.Value{ .bool = value },
    //             else => @compileError("Unsupported parameter type: " ++ @typeName(@TypeOf(value))),
    //         };
    //         try json_map.put(field.name, json_value);
    //     }
    //
    //     const json_object = std.json.Value{ .object = json_map };
    //     var buffer = std.ArrayList(u8).init(self.allocator);
    //     try std.json.stringify(json_object, .{}, buffer.writer());
    //     return buffer.toOwnedSlice();
    // }
    //
    // pub fn init(allocator: Allocator, api_id: i32, api_hash: []const u8, target_chat_id: i64, config: Config) !Self {
    //     return Self{
    //         .allocator = allocator,
    //         .api_id = api_id,
    //         .api_hash = api_hash,
    //         .client_id = 0,
    //         .is_authorized = false,
    //         .is_closed = false,
    //         .target_chat_id = target_chat_id,
    //         .config = config,
    //         .send_buffer = std.ArrayList(u8).init(allocator),
    //         .my_user_info = null,
    //         .user_cache = std.HashMap(i64, UserInfo, std.hash_map.AutoContext(i64), std.hash_map.default_max_load_percentage).init(allocator),
    //         .pending_callbacks = std.HashMap([]const u8, PendingCallback, std.hash_map.StringContext, std.hash_map.default_max_load_percentage).init(allocator),
    //         .request_counter = std.atomic.Value(u64).init(0),
    //         .event_handlers = EventHandlers.init(allocator),
    //     };
    // }
    //
    // pub fn deinit(self: *Self) void {
    //     self.send_buffer.deinit();
    //     var user_iter = self.user_cache.iterator();
    //     while (user_iter.next()) |entry| {
    //         var mutable_entry = entry;
    //         mutable_entry.value_ptr.deinit(self.allocator);
    //     }
    //     self.user_cache.deinit();
    //     var callback_iter = self.pending_callbacks.iterator();
    //     while (callback_iter.next()) |entry| {
    //         var mutable_entry = entry;
    //         mutable_entry.value_ptr.deinit();
    //     }
    //     self.pending_callbacks.deinit();
    //     self.event_handlers.deinit();
    // }
    //
    // pub fn start(self: *Self) !void {
    //     td_set_log_verbosity_level(@as(c_int, self.config.log_verbosity));
    //
    //     self.client_id = td_create_client_id();
    //     if (self.client_id == 0) {
    //         return TelegramError.ClientNotInitialized;
    //     }
    //
    //     const parameters = .{
    //         .database_directory = self.config.database_directory,
    //         .files_directory = self.config.files_directory,
    //         .use_test_dc = self.config.use_test_dc,
    //         .use_message_database = true,
    //         .use_secret_chats = false,
    //         .system_language_code = self.config.system_language_code,
    //         .device_model = self.config.device_model,
    //         .system_version = self.config.system_version,
    //         .application_version = self.config.application_version,
    //         .enable_storage_optimizer = self.config.enable_storage_optimizer,
    //         .api_id = self.api_id,
    //         .api_hash = self.api_hash,
    //     };
    //
    //     const request = try self.buildJsonRequest("setTdlibParameters", parameters);
    //     defer self.allocator.free(request);
    //
    //     try self.send(request);
    // }
    //
    // /// Register a handler for a specific event type.
    // pub fn addEventHandler(self: *Self, event_type: EventType, handler: HandlerFn, ctx: ?*anyopaque) !void {
    //     const entry = HandlerEntry{ .handler = handler, .ctx = ctx };
    //     switch (event_type) {
    //         .new_message => try self.event_handlers.on_message.append(entry),
    //         .edited_message => try self.event_handlers.on_edited_message.append(entry),
    //         .user_update => try self.event_handlers.on_user_update.append(entry),
    //         .file_update => try self.event_handlers.on_file_update.append(entry),
    //         .new_chat => try self.event_handlers.on_new_chat.append(entry),
    //         .auth_update => try self.event_handlers.on_auth_update.append(entry),
    //         .unknown => try self.event_handlers.on_unknown.append(entry),
    //     }
    // }
    //
    // pub fn send(self: *Self, request: []const u8) !void {
    //     if (self.is_closed) {
    //         return TelegramError.ClientClosed;
    //     }
    //     if (self.client_id == 0) {
    //         return TelegramError.ClientNotInitialized;
    //     }
    //
    //     self.send_buffer.clearRetainingCapacity();
    //     try self.send_buffer.appendSlice(request);
    //     try self.send_buffer.append(0);
    //
    //     const null_terminated_ptr: [*:0]const u8 = @ptrCast(self.send_buffer.items.ptr);
    //     td_send(self.client_id, null_terminated_ptr);
    //
    //     if (self.config.debug_mode) {
    //         log.debug("Sent request: {s}", .{request});
    //     }
    // }
    //
    // pub fn receive(self: *Self) ?[]const u8 {
    //     if (self.is_closed) return null;
    //
    //     if (td_receive(self.config.receive_timeout)) |result| {
    //         return std.mem.span(result);
    //     }
    //
    //     return null;
    // }
    //
    // fn downloadFileCallback(self: *Self, file_id: i64, chat_id: i64, user_id: i64) anyerror!void {
    //     _ = chat_id;
    //     _ = user_id;
    //     const params = .{
    //         .file_id = file_id,
    //         .priority = @as(i32, 32),
    //         .offset = @as(i32, 0),
    //         .limit = @as(i32, 0),
    //         .synchronous = true,
    //     };
    //     const request = try self.buildJsonRequest("downloadFile", params);
    //     defer self.allocator.free(request);
    //     try self.send(request);
    // }
    //
    // pub fn tick(self: *Self) !bool {
    //     if (self.is_closed) return false;
    //
    //     if (self.receive()) |result| {
    //         var arena = std.heap.ArenaAllocator.init(self.allocator);
    //         defer arena.deinit();
    //
    //         const event = try telegram_event.parseTelegramEvent(
    //             arena,
    //             result,
    //         );
    //         const dispatch = struct {
    //             fn callAll(list: *std.ArrayList(HandlerEntry), event_ptr: *const telegram_event.TelegramEvent) void {
    //                 for (list.items) |entry| {
    //                     entry.handler(entry.ctx, event_ptr);
    //                 }
    //             }
    //         };
    //         switch (event) {
    //             .new_message => dispatch.callAll(&self.event_handlers.on_message, &event),
    //             // .edited_message => dispatch.callAll(&self.event_handlers.on_edited_message, &event),
    //             // .user_update => dispatch.callAll(&self.event_handlers.on_user_update, &event),
    //             // .file_update => dispatch.callAll(&self.event_handlers.on_file_update, &event),
    //             // .new_chat => dispatch.callAll(&self.event_handlers.on_new_chat, &event),
    //             .auth_update => dispatch.callAll(&self.event_handlers.on_auth_update, &event),
    //             .unknown => dispatch.callAll(&self.event_handlers.on_unknown, &event),
    //             else => {
    //                 log.warn("Unsupported event", .{});
    //             },
    //         }
    //     }
    //
    //     return true;
    // }
    //
    // pub fn sendMessage(self: *Self, chat_id: i64, text: []const u8) !void {
    //     const request = try std.fmt.allocPrint(self.allocator, "{{\"@type\":\"sendMessage\",\"chat_id\":{d},\"input_message_content\":{{\"@type\":\"inputMessageText\",\"text\":{{\"@type\":\"formattedText\",\"text\":\"{s}\"}}}}}}", .{ chat_id, text });
    //     defer self.allocator.free(request);
    //     try self.send(request);
    //     if (self.config.debug_mode) {
    //         log.info("Sent message to chat {d}: {s}", .{ chat_id, text });
    //     }
    // }
    //
    // pub fn sendMessageWithCallback(self: *Self, chat_id: i64, text: []const u8, callback: ?Callback, context: ?*anyopaque) !void {
    //     const params = .{
    //         .chat_id = chat_id,
    //         .input_message_content = .{
    //             .@"@type" = "inputMessageText",
    //             .text = .{
    //                 .@"@type" = "formattedText",
    //                 .text = text,
    //             },
    //         },
    //     };
    //     try self.sendWithCallback("sendMessage", params, callback, context);
    // }
    //
    // pub fn getMeWithCallback(self: *Self, callback: Callback, context: ?*anyopaque) !void {
    //     const params = .{};
    //     try self.sendWithCallback("getMe", params, callback, context);
    // }
    //
    // pub fn getChatWithCallback(self: *Self, chat_id: i64, callback: Callback, context: ?*anyopaque) !void {
    //     const params = .{ .chat_id = chat_id };
    //     try self.sendWithCallback("getChat", params, callback, context);
    // }
    //
    // pub fn getUserWithCallback(self: *Self, user_id: i64, callback: Callback, context: ?*anyopaque) !void {
    //     const params = .{ .user_id = user_id };
    //     try self.sendWithCallback("getUser", params, callback, context);
    // }
    //
    // pub fn downloadFileWithCallback(self: *Self, file_id: i64, callback: Callback, context: ?*anyopaque) !void {
    //     const params = .{
    //         .file_id = file_id,
    //         .priority = @as(i32, 32),
    //         .offset = @as(i32, 0),
    //         .limit = @as(i32, 0),
    //         .synchronous = true,
    //     };
    //     try self.sendWithCallback("downloadFile", params, callback, context);
    // }
    //
    // fn generateRequestId(self: *Self) ![]u8 {
    //     const counter = self.request_counter.fetchAdd(1, .seq_cst);
    //     return std.fmt.allocPrint(self.allocator, "req_{d}_{d}", .{ std.time.timestamp(), counter });
    // }
    //
    // fn sendWithCallback(self: *Self, request_type: []const u8, params: anytype, callback: ?Callback, context: ?*anyopaque) !void {
    //     var json_map = std.json.ObjectMap.init(self.allocator);
    //     defer json_map.deinit();
    //     try json_map.put("@type", std.json.Value{ .string = request_type });
    //     var request_id: ?[]u8 = null;
    //     if (callback) |_| {
    //         request_id = try self.generateRequestId();
    //         try json_map.put("@extra", std.json.Value{ .string = request_id.? });
    //     }
    //     inline for (std.meta.fields(@TypeOf(params))) |field| {
    //         const value = @field(params, field.name);
    //         const json_value = switch (@TypeOf(value)) {
    //             []const u8, []u8 => std.json.Value{ .string = value },
    //             i32, i64 => std.json.Value{ .integer = @intCast(value) },
    //             bool => std.json.Value{ .bool = value },
    //             else => @compileError("Unsupported parameter type: " ++ @typeName(@TypeOf(value))),
    //         };
    //         try json_map.put(field.name, json_value);
    //     }
    //     const json_object = std.json.Value{ .object = json_map };
    //     var buffer = std.ArrayList(u8).init(self.allocator);
    //     defer buffer.deinit();
    //     try std.json.stringify(json_object, .{}, buffer.writer());
    //     if (callback) |cb| {
    //         const pending = PendingCallback{
    //             .callback = cb,
    //             .context = context,
    //             .request_id = try self.allocator.dupe(u8, request_id.?),
    //             .allocator = self.allocator,
    //         };
    //         try self.pending_callbacks.put(try self.allocator.dupe(u8, request_id.?), pending);
    //     }
    //     try self.send(buffer.items);
    //     if (request_id) |rid| {
    //         self.allocator.free(rid);
    //     }
    // }
};
