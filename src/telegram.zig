const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const c = std.c;

extern "c" fn td_create_client_id() c_int;
extern "c" fn td_send(client_id: c_int, request: [*:0]const u8) void;
extern "c" fn td_receive(timeout: f64) ?[*:0]const u8;
extern "c" fn td_execute(request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_set_log_message_callback(max_verbosity_level: c_int, callback: ?*const fn (c_int, [*:0]const u8) callconv(.C) void) void;

extern "c" fn td_json_client_create() ?*anyopaque;
extern "c" fn td_json_client_send(client: *anyopaque, request: [*:0]const u8) void;
extern "c" fn td_json_client_receive(client: *anyopaque, timeout: f64) ?[*:0]const u8;
extern "c" fn td_json_client_execute(client: ?*anyopaque, request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_json_client_destroy(client: *anyopaque) void;

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
    InvalidState,
    JsonParseError,
    AllocationError,
};

pub const MessageHandler = *const fn (chat_id: i64, user_id: i64, message_text: []const u8) void;

pub const TelegramClient = struct {
    allocator: Allocator,
    api_id: i32,
    api_hash: []const u8,
    client_id: c_int,
    is_authorized: bool,
    is_closed: bool,
    target_chat_id: ?i64,
    config: Config,
    send_buffer: std.ArrayList(u8),
    message_handler: ?MessageHandler,
    
    const Self = @This();

    pub fn init(allocator: Allocator, api_id: i32, api_hash: []const u8, target_chat_id: ?i64, config: Config) !Self {
        return Self{
            .allocator = allocator,
            .api_id = api_id,
            .api_hash = api_hash,
            .client_id = 0,
            .is_authorized = false,
            .is_closed = false,
            .target_chat_id = target_chat_id,
            .config = config,
            .send_buffer = std.ArrayList(u8).init(allocator),
            .message_handler = null,
        };
    }

    pub fn deinit(self: *Self) void {
        if (!self.is_closed) {
            // Try to close gracefully
            self.send("{\"@type\":\"close\"}") catch {};
        }
        self.send_buffer.deinit();
    }

    pub fn setMessageHandler(self: *Self, handler: MessageHandler) void {
        self.message_handler = handler;
    }

    pub fn start(self: *Self) !void {
        // Set log verbosity level
        const log_request = try std.fmt.allocPrintZ(self.allocator, "{{\"@type\":\"setLogVerbosityLevel\",\"new_verbosity_level\":{d}}}", .{self.config.log_verbosity});
        defer self.allocator.free(log_request);
        
        _ = td_execute(log_request.ptr);
        
        self.client_id = td_create_client_id();
        if (self.client_id == 0) {
            return TelegramError.ClientNotInitialized;
        }
        
        if (self.config.debug_mode) {
            print("Created TDLib client with ID: {d}\n", .{self.client_id});
        }
        
        // Get TDLib version
        const version_request = "{\"@type\":\"getOption\",\"name\":\"version\"}";
        try self.send(version_request);
    }

    pub fn send(self: *Self, request: []const u8) !void {
        if (self.is_closed) {
            return TelegramError.ClientClosed;
        }
        if (self.client_id == 0) {
            return TelegramError.ClientNotInitialized;
        }
        
        self.send_buffer.clearRetainingCapacity();
        try self.send_buffer.appendSlice(request);
        try self.send_buffer.append(0); // null terminator
        
        const null_terminated_ptr: [*:0]const u8 = @ptrCast(self.send_buffer.items.ptr);
        td_send(self.client_id, null_terminated_ptr);
        
        if (self.config.debug_mode) {
            print("Sent request: {s}\n", .{request});
        }
    }

    pub fn receive(self: *Self, timeout: f64) ?[]const u8 {
        if (self.is_closed) return null;
        
        if (td_receive(timeout)) |result| {
            return std.mem.span(result);
        }
        return null;
    }

    fn buildJsonRequest(self: *Self, request_type: []const u8, params: anytype) ![]u8 {
        var buffer = std.ArrayList(u8).init(self.allocator);
        const writer = buffer.writer();
        
        try writer.print("{{\"@type\":\"{s}\"", .{request_type});
        
        // Add parameters dynamically based on the struct fields
        inline for (std.meta.fields(@TypeOf(params))) |field| {
            try writer.print(",\"{s}\":", .{field.name});
            const value = @field(params, field.name);
            switch (@TypeOf(value)) {
                []const u8 => try writer.print("\"{s}\"", .{value}),
                i32, i64 => try writer.print("{d}", .{value}),
                bool => try writer.print("{}", .{value}),
                else => @compileError("Unsupported parameter type: " ++ @typeName(@TypeOf(value))),
            }
        }
        
        try writer.writeByte('}');
        return buffer.toOwnedSlice();
    }

    pub fn setTdlibParameters(self: *Self) !void {
        const params = struct {
            use_test_dc: bool,
            database_directory: []const u8,
            files_directory: []const u8,
            database_encryption_key: []const u8,
            use_file_database: bool,
            use_chat_info_database: bool,
            use_message_database: bool,
            use_secret_chats: bool,
            api_id: i32,
            api_hash: []const u8,
            system_language_code: []const u8,
            device_model: []const u8,
            system_version: []const u8,
            application_version: []const u8,
            enable_storage_optimizer: bool,
            ignore_file_names: bool,
        }{
            .use_test_dc = self.config.use_test_dc,
            .database_directory = self.config.database_directory,
            .files_directory = self.config.files_directory,
            .database_encryption_key = "",
            .use_file_database = true,
            .use_chat_info_database = true,
            .use_message_database = true,
            .use_secret_chats = true,
            .api_id = self.api_id,
            .api_hash = self.api_hash,
            .system_language_code = self.config.system_language_code,
            .device_model = self.config.device_model,
            .system_version = self.config.system_version,
            .application_version = self.config.application_version,
            .enable_storage_optimizer = self.config.enable_storage_optimizer,
            .ignore_file_names = false,
        };
        
        const request = try self.buildJsonRequest("setTdlibParameters", params);
        defer self.allocator.free(request);
        
        try self.send(request);
    }

    pub fn setAuthenticationPhoneNumber(self: *Self, phone_number: []const u8) !void {
        const params = struct {
            phone_number: []const u8,
        }{ .phone_number = phone_number };
        
        const request = try self.buildJsonRequest("setAuthenticationPhoneNumber", params);
        defer self.allocator.free(request);
        
        try self.send(request);
    }

    pub fn checkAuthenticationCode(self: *Self, code: []const u8) !void {
        const params = struct {
            code: []const u8,
        }{ .code = code };
        
        const request = try self.buildJsonRequest("checkAuthenticationCode", params);
        defer self.allocator.free(request);
        
        try self.send(request);
    }

    pub fn checkAuthenticationPassword(self: *Self, password: []const u8) !void {
        const params = struct {
            password: []const u8,
        }{ .password = password };
        
        const request = try self.buildJsonRequest("checkAuthenticationPassword", params);
        defer self.allocator.free(request);
        
        try self.send(request);
    }

    pub fn registerUser(self: *Self, first_name: []const u8, last_name: []const u8) !void {
        const params = struct {
            first_name: []const u8,
            last_name: []const u8,
        }{ .first_name = first_name, .last_name = last_name };
        
        const request = try self.buildJsonRequest("registerUser", params);
        defer self.allocator.free(request);
        
        try self.send(request);
    }

    pub fn openSpecificChat(self: *Self, chat_id: i64) !void {
        const params = struct {
            chat_id: i64,
        }{ .chat_id = chat_id };
        
        const request = try self.buildJsonRequest("openChat", params);
        defer self.allocator.free(request);
        
        try self.send(request);
        
        if (self.config.debug_mode) {
            print("Opened chat {d} for monitoring\n", .{chat_id});
        }
    }

    pub fn processUpdate(self: *Self, update_json: []const u8) !void {
        const parsed = std.json.parseFromSlice(std.json.Value, self.allocator, update_json, .{}) catch |err| {
            if (self.config.debug_mode) {
                print("Failed to parse JSON: {}\n", .{err});
            }
            return TelegramError.JsonParseError;
        };
        defer parsed.deinit();
        
        const root = parsed.value.object;
        
        if (root.get("@type")) |type_value| {
            const update_type = type_value.string;
            
            // Only show very important updates to reduce noise
            const show_update = self.config.debug_mode and (
                std.mem.eql(u8, update_type, "updateAuthorizationState") or
                std.mem.eql(u8, update_type, "updateNewMessage") or
                std.mem.eql(u8, update_type, "updateConnectionState") or
                std.mem.eql(u8, update_type, "chats") or
                std.mem.eql(u8, update_type, "user") or
                std.mem.eql(u8, update_type, "error"));
            
            if (show_update) {
                print("Update type: {s}\n", .{update_type});
            }
            
            try self.dispatchUpdate(update_type, root);
        }
    }

    fn dispatchUpdate(self: *Self, update_type: []const u8, root: std.json.ObjectMap) !void {
        if (std.mem.eql(u8, update_type, "updateAuthorizationState")) {
            try self.handleAuthorizationState(root);
        } else if (std.mem.eql(u8, update_type, "updateNewMessage")) {
            try self.handleNewMessage(root);
        } else if (std.mem.eql(u8, update_type, "updateConnectionState")) {
            try self.handleConnectionState(root);
        } else if (std.mem.eql(u8, update_type, "option")) {
            try self.handleOption(root);
        } else if (std.mem.eql(u8, update_type, "chats")) {
            try self.handleChats(root);
        } else if (std.mem.eql(u8, update_type, "user")) {
            try self.handleUser(root);
        } else if (std.mem.eql(u8, update_type, "error")) {
            try self.handleError(root);
        }
        // Silently ignore unknown update types
    }

    fn handleAuthorizationState(self: *Self, root: std.json.ObjectMap) !void {
        if (root.get("authorization_state")) |auth_state| {
            const state_obj = auth_state.object;
            if (state_obj.get("@type")) |state_type| {
                const state_name = state_type.string;
                print("[Telegram] Authorization state: {s}\n", .{state_name});
                
                if (std.mem.eql(u8, state_name, "authorizationStateWaitTdlibParameters")) {
                    print("[Telegram] Setting TDLib parameters...\n", .{});
                    try self.setTdlibParameters();
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitPhoneNumber")) {
                    print("[Telegram] Please enter your phone number (with country code, e.g., +1234567890): ", .{});
                    const phone = try self.readInput();
                    defer self.allocator.free(phone);
                    try self.setAuthenticationPhoneNumber(phone);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitCode")) {
                    print("[Telegram] Please enter the authentication code: ", .{});
                    const code = try self.readInput();
                    defer self.allocator.free(code);
                    try self.checkAuthenticationCode(code);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitPassword")) {
                    print("[Telegram] Please enter your 2FA password: ", .{});
                    const password = try self.readInput();
                    defer self.allocator.free(password);
                    try self.checkAuthenticationPassword(password);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitRegistration")) {
                    print("[Telegram] Please enter your first name: ", .{});
                    const first_name = try self.readInput();
                    defer self.allocator.free(first_name);
                    print("[Telegram] Please enter your last name: ", .{});
                    const last_name = try self.readInput();
                    defer self.allocator.free(last_name);
                    try self.registerUser(first_name, last_name);
                } else if (std.mem.eql(u8, state_name, "authorizationStateReady")) {
                    print("[Telegram] Authorization successful!\n", .{});
                    self.is_authorized = true;
                    
                    try self.send("{\"@type\":\"getMe\"}");
                    
                    if (self.target_chat_id) |chat_id| {
                        print("[Telegram] Monitoring specific chat: {d}\n", .{chat_id});
                        try self.openSpecificChat(chat_id);
                    } else {
                        print("[Telegram] Getting all chats...\n", .{});
                        try self.send("{\"@type\":\"getChats\",\"limit\":20}");
                    }
                } else if (std.mem.eql(u8, state_name, "authorizationStateClosed")) {
                    print("[Telegram] TDLib client closed\n", .{});
                    self.is_closed = true;
                }
            }
        }
    }

    fn handleNewMessage(self: *Self, root: std.json.ObjectMap) !void {
        if (root.get("message")) |message| {
            const msg_obj = message.object;
            
            var message_chat_id: i64 = 0;
            if (msg_obj.get("chat_id")) |chat_id| {
                message_chat_id = chat_id.integer;
            }
            
            // If we're monitoring a specific chat, only show messages from that chat
            if (self.target_chat_id) |target_id| {
                if (message_chat_id != target_id) {
                    return;
                }
            }
            
            var user_id: i64 = 0;
            if (msg_obj.get("sender_id")) |sender_id| {
                const sender_obj = sender_id.object;
                if (sender_obj.get("user_id")) |uid| {
                    user_id = uid.integer;
                }
            }
            
            var message_text: []const u8 = "";
            if (msg_obj.get("content")) |content| {
                const content_obj = content.object;
                if (content_obj.get("text")) |text| {
                    const text_obj = text.object;
                    if (text_obj.get("text")) |text_content| {
                        message_text = text_content.string;
                    }
                }
            }
            
            print("[Telegram] NEW MESSAGE: Chat {d}, User {d}: {s}\n", .{ message_chat_id, user_id, message_text });
            
            // Call the message handler if set
            if (self.message_handler) |handler| {
                handler(message_chat_id, user_id, message_text);
            }
        }
    }

    fn handleConnectionState(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("state")) |state| {
            const state_obj = state.object;
            if (state_obj.get("@type")) |state_type| {
                print("[Telegram] Connection state: {s}\n", .{state_type.string});
            }
        }
    }

    fn handleOption(self: *Self, root: std.json.ObjectMap) !void {
        if (self.config.debug_mode) {
            if (root.get("name")) |name| {
                if (root.get("value")) |value| {
                    const value_obj = value.object;
                    if (value_obj.get("value")) |actual_value| {
                        print("[Telegram] Option {s}: {s}\n", .{ name.string, actual_value.string });
                    }
                }
            }
        }
    }

    fn handleChats(self: *Self, root: std.json.ObjectMap) !void {
        print("[Telegram] Received chat list\n", .{});
        if (root.get("chat_ids")) |chat_ids| {
            const chat_array = chat_ids.array;
            print("[Telegram] Found {d} chats, opening them to receive messages...\n", .{chat_array.items.len});
            
            for (chat_array.items) |chat_id_value| {
                const chat_id = chat_id_value.integer;
                
                // Open each chat to receive messages in real-time
                try self.openSpecificChat(chat_id);
                
                if (self.config.debug_mode) {
                    print("[Telegram] Opened chat {d}\n", .{chat_id});
                }
                
                // Small delay between opening chats
                std.time.sleep(100 * std.time.ns_per_ms);
            }
            
            print("[Telegram] All chats opened! You should now receive messages in real-time.\n", .{});
        } else {
            print("[Telegram] No chat_ids found in response\n", .{});
        }
    }

    fn handleUser(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("first_name")) |first_name| {
            if (root.get("username")) |username| {
                print("[Telegram] Logged in as: {s} (@{s})\n", .{ first_name.string, username.string });
            } else {
                print("[Telegram] Logged in as: {s}\n", .{first_name.string});
            }
        }
    }

    fn handleError(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("message")) |message| {
            if (root.get("code")) |code| {
                print("[Telegram] Error {d}: {s}\n", .{ code.integer, message.string });
            } else {
                print("[Telegram] Error: {s}\n", .{message.string});
            }
        } else {
            print("[Telegram] Unknown error occurred\n", .{});
        }
    }

    fn readInput(self: *Self) ![]u8 {
        const stdin = std.io.getStdIn().reader();
        const input = try stdin.readUntilDelimiterAlloc(self.allocator, '\n', 1024);
        
        // Trim whitespace
        const trimmed = std.mem.trim(u8, input, " \t\r\n");
        defer self.allocator.free(input);
        return try self.allocator.dupe(u8, trimmed);
    }

    pub fn tick(self: *Self) !bool {
        if (self.is_closed) return false;
        
        if (self.receive(self.config.receive_timeout)) |result| {
            try self.processUpdate(result);
        }
        
        return true;
    }
}; 