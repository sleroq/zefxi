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
    avatar_base_url: []const u8 = "http://127.0.0.1:8080",
};

pub const TelegramError = error{
    ClientNotInitialized,
    ClientClosed,
    InvalidState,
    JsonParseError,
    AllocationError,
};

// Structure to hold user information for Discord spoofing
pub const UserInfo = struct {
    user_id: i64,
    first_name: []const u8,
    last_name: ?[]const u8,
    username: ?[]const u8,
    avatar_url: ?[]const u8,
    
    pub fn getDisplayName(self: UserInfo, allocator: Allocator) ![]u8 {
        if (self.last_name) |lname| {
            return std.fmt.allocPrint(allocator, "{s} {s}", .{ self.first_name, lname });
        } else {
            return std.fmt.allocPrint(allocator, "{s}", .{self.first_name});
        }
    }
};

// Attachment types supported by the bridge
pub const AttachmentType = enum {
    photo,
    document,
    video,
    audio,
    voice,
    video_note,
    sticker,
    animation,
};

// Structure to hold attachment information for bridging
pub const AttachmentInfo = struct {
    file_id: i32,
    attachment_type: AttachmentType,
    width: ?i32 = null,
    height: ?i32 = null,
    duration: ?i32 = null,
    file_size: ?i64 = null,
    file_name: ?[]const u8 = null,
    mime_type: ?[]const u8 = null,
    caption: ?[]const u8 = null,
    local_path: ?[]const u8 = null,
    url: ?[]const u8 = null,
    
    pub fn deinit(self: *AttachmentInfo, allocator: Allocator) void {
        if (self.file_name) |name| {
            allocator.free(name);
        }
        if (self.mime_type) |mime| {
            allocator.free(mime);
        }
        if (self.caption) |caption| {
            allocator.free(caption);
        }
        if (self.local_path) |path| {
            allocator.free(path);
        }
        if (self.url) |url| {
            allocator.free(url);
        }
    }
    
    pub fn getDisplayName(self: AttachmentInfo) []const u8 {
        return switch (self.attachment_type) {
            .photo => "Photo",
            .document => if (self.file_name) |name| name else "Document",
            .video => "Video",
            .audio => "Audio",
            .voice => "Voice Message",
            .video_note => "Video Note",
            .sticker => "Sticker",
            .animation => "GIF",
        };
    }
    
    pub fn getEmoji(self: AttachmentInfo) []const u8 {
        return switch (self.attachment_type) {
            .photo => "📸",
            .document => "📄",
            .video => "🎥",
            .audio => "🎵",
            .voice => "🎤",
            .video_note => "📹",
            .sticker => "🎭",
            .animation => "🎞️",
        };
    }
};

// Message content types
pub const MessageContent = union(enum) {
    text: []const u8,
    attachment: AttachmentInfo,
    text_with_attachment: struct {
        text: []const u8,
        attachment: AttachmentInfo,
    },
};

// Pending attachment message for when download completes
pub const PendingAttachmentMessage = struct {
    chat_id: i64,
    user_info: UserInfo,
    content: MessageContent,
    
    pub fn deinit(self: *PendingAttachmentMessage, allocator: Allocator) void {
        // Clean up user info
        allocator.free(self.user_info.first_name);
        if (self.user_info.last_name) |lname| {
            allocator.free(lname);
        }
        if (self.user_info.username) |uname| {
            allocator.free(uname);
        }
        if (self.user_info.avatar_url) |avatar| {
            allocator.free(avatar);
        }
        
        // Clean up content
        switch (self.content) {
            .attachment => |*attachment| attachment.deinit(allocator),
            .text_with_attachment => |*text_attachment| text_attachment.attachment.deinit(allocator),
            .text => {},
        }
    }
};

pub const MessageHandler = *const fn (ctx: *anyopaque, chat_id: i64, user_info: UserInfo, content: MessageContent) void;

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
    message_handler_ctx: ?*anyopaque,
    my_user_id: ?i64,
    user_cache: std.HashMap(i64, UserInfo, std.hash_map.AutoContext(i64), std.hash_map.default_max_load_percentage),
    pending_requests: std.HashMap([]const u8, i64, std.hash_map.StringContext, std.hash_map.default_max_load_percentage),
    pending_images: std.HashMap(i32, PendingAttachmentMessage, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage),
    file_id_to_path: std.HashMap(i32, []const u8, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage),
    
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
            .message_handler_ctx = null,
            .my_user_id = null,
            .user_cache = std.HashMap(i64, UserInfo, std.hash_map.AutoContext(i64), std.hash_map.default_max_load_percentage).init(allocator),
            .pending_requests = std.HashMap([]const u8, i64, std.hash_map.StringContext, std.hash_map.default_max_load_percentage).init(allocator),
            .pending_images = std.HashMap(i32, PendingAttachmentMessage, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage).init(allocator),
            .file_id_to_path = std.HashMap(i32, []const u8, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage).init(allocator),
        };
    }

    pub fn deinit(self: *Self) void {
        if (!self.is_closed) {
            // Try to close gracefully
            self.send("{\"@type\":\"close\"}") catch {};
        }
        self.send_buffer.deinit();
        
        // Clean up user cache
        var iterator = self.user_cache.iterator();
        while (iterator.next()) |entry| {
            self.allocator.free(entry.value_ptr.first_name);
            if (entry.value_ptr.last_name) |lname| {
                self.allocator.free(lname);
            }
            if (entry.value_ptr.username) |uname| {
                self.allocator.free(uname);
            }
            if (entry.value_ptr.avatar_url) |avatar| {
                self.allocator.free(avatar);
            }
        }
        self.user_cache.deinit();
        
        // Clean up pending requests
        var req_iterator = self.pending_requests.iterator();
        while (req_iterator.next()) |entry| {
            self.allocator.free(entry.key_ptr.*);
        }
        self.pending_requests.deinit();
        
        // Clean up pending images
        var img_iterator = self.pending_images.iterator();
        while (img_iterator.next()) |entry| {
            entry.value_ptr.deinit(self.allocator);
        }
        self.pending_images.deinit();
        
        // Clean up file ID to path mapping
        var file_iterator = self.file_id_to_path.iterator();
        while (file_iterator.next()) |entry| {
            self.allocator.free(entry.value_ptr.*);
        }
        self.file_id_to_path.deinit();
    }

    pub fn setMessageHandler(self: *Self, ctx: *anyopaque, handler: MessageHandler) void {
        self.message_handler = handler;
        self.message_handler_ctx = ctx;
    }

    pub fn getUserInfo(self: *Self, user_id: i64) ?UserInfo {
        return self.user_cache.get(user_id);
    }

    pub fn requestUserInfo(self: *Self, user_id: i64) !void {
        const request = try std.fmt.allocPrint(self.allocator, "{{\"@type\":\"getUser\",\"user_id\":{d},\"@extra\":\"user_{d}\"}}", .{ user_id, user_id });
        defer self.allocator.free(request);
        
        try self.send(request);
        
        print("[Telegram] 📤 Requested user info for {d}\n", .{user_id});
        if (self.config.debug_mode) {
            print("[Telegram] User info request: {s}\n", .{request});
        }
    }

    pub fn requestUserProfilePhotos(self: *Self, user_id: i64) !void {
        const request = try std.fmt.allocPrint(self.allocator, "{{\"@type\":\"getUserProfilePhotos\",\"user_id\":{d},\"offset\":0,\"limit\":1,\"@extra\":\"photos_{d}\"}}", .{ user_id, user_id });
        defer self.allocator.free(request);
        
        try self.send(request);
        
        if (self.config.debug_mode) {
            print("[Telegram] Requested profile photos for user {d}\n", .{user_id});
            print("[Telegram] Profile photos request: {s}\n", .{request});
        }
    }

    pub fn downloadFile(self: *Self, file_id: i32, priority: i32, extra: []const u8) !void {
        const request = try std.fmt.allocPrint(self.allocator, "{{\"@type\":\"downloadFile\",\"file_id\":{d},\"priority\":{d},\"offset\":0,\"limit\":0,\"synchronous\":true,\"@extra\":\"{s}\"}}", .{ file_id, priority, extra });
        defer self.allocator.free(request);
        
        try self.send(request);
        
        print("[Telegram] 📥 Requested file download for file_id {d} with extra: {s}\n", .{ file_id, extra });
        if (self.config.debug_mode) {
            print("[Telegram] Download request: {s}\n", .{request});
        }
    }

    pub fn getAvatarLocalPath(self: *Self, user_id: i64) ?[]const u8 {
        if (self.user_cache.get(user_id)) |user_info| {
            return user_info.avatar_url;
        }
        return null;
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
        } else if (std.mem.eql(u8, update_type, "userProfilePhotos")) {
            print("[Telegram] 📸 Received userProfilePhotos update\n", .{});
            try self.handleUserProfilePhotos(root);
        } else if (std.mem.eql(u8, update_type, "chatPhotos")) {
            print("[Telegram] 📸 Received chatPhotos update\n", .{});
            try self.handleChatPhotos(root);
        } else if (std.mem.eql(u8, update_type, "file")) {
            print("[Telegram] 📁 Received file update\n", .{});
            try self.handleFile(root);
        } else if (std.mem.eql(u8, update_type, "error")) {
            try self.handleError(root);
        } else {
            // Log unknown update types when debug mode is on
            if (self.config.debug_mode) {
                print("[Telegram] 🔍 Unknown update type: {s}\n", .{update_type});
            }
        }
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
            
            // Log message processing if debug mode is enabled
            if (self.config.debug_mode) {
                print("[Telegram] Processing new message\n", .{});
            }
            
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
            
            // Ignore messages from myself
            if (self.my_user_id) |my_id| {
                if (user_id == my_id) {
                    if (self.config.debug_mode) {
                        print("[Telegram] Ignoring message from myself (user {d})\n", .{my_id});
                    }
                    return;
                }
            }
            
            // Parse message content (text, photo, or both)
            var message_content: ?MessageContent = null;
            var message_text: []const u8 = "";
            
            if (msg_obj.get("content")) |content| {
                const content_obj = content.object;
                
                if (content_obj.get("@type")) |content_type| {
                    const type_str = content_type.string;
                    
                    if (std.mem.eql(u8, type_str, "messageText")) {
                        // Text message
                        if (content_obj.get("text")) |text| {
                            const text_obj = text.object;
                            if (text_obj.get("text")) |text_content| {
                                message_text = text_content.string;
                                message_content = MessageContent{ .text = message_text };
                            }
                        }
                    } else if (std.mem.eql(u8, type_str, "messagePhoto")) {
                        // Photo message
                        message_content = try self.parsePhotoMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageDocument")) {
                        // Document message
                        message_content = try self.parseDocumentMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageVideo")) {
                        // Video message
                        message_content = try self.parseVideoMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageAudio")) {
                        // Audio message
                        message_content = try self.parseAudioMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageVoiceNote")) {
                        // Voice note message
                        message_content = try self.parseVoiceMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageVideoNote")) {
                        // Video note message
                        message_content = try self.parseVideoNoteMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageSticker")) {
                        // Sticker message
                        message_content = try self.parseStickerMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else if (std.mem.eql(u8, type_str, "messageAnimation")) {
                        // Animation/GIF message
                        message_content = try self.parseAnimationMessage(content_obj, &message_text, message_chat_id, user_id);
                    } else {
                        // Other message types - treat as text for now
                        if (self.config.debug_mode) {
                            print("[Telegram] 🔍 Unsupported message type: {s}\n", .{type_str});
                        }
                        message_content = MessageContent{ .text = "" };
                    }
                }
            }
            
            // Default to empty text if no content was parsed
            if (message_content == null) {
                message_content = MessageContent{ .text = "" };
            }
            
            print("[Telegram] NEW MESSAGE: Chat {d}, User {d}: {s}\n", .{ message_chat_id, user_id, message_text });
            
            // Get or request user info
            if (self.getUserInfo(user_id)) |user_info| {
                // We have user info, check if we need to wait for image download
                const should_wait_for_image = switch (message_content.?) {
                    .attachment => |attachment| attachment.url == null,
                    .text_with_attachment => |text_attachment| text_attachment.attachment.url == null,
                    .text => false,
                };
                
                if (should_wait_for_image) {
                    // Store pending image message
                    const file_id = switch (message_content.?) {
                        .attachment => |attachment| attachment.file_id,
                        .text_with_attachment => |text_attachment| text_attachment.attachment.file_id,
                        .text => unreachable,
                    };
                    
                    // Clone user info for storage
                    const cloned_user = UserInfo{
                        .user_id = user_info.user_id,
                        .first_name = try self.allocator.dupe(u8, user_info.first_name),
                        .last_name = if (user_info.last_name) |lname| try self.allocator.dupe(u8, lname) else null,
                        .username = if (user_info.username) |uname| try self.allocator.dupe(u8, uname) else null,
                        .avatar_url = if (user_info.avatar_url) |avatar| try self.allocator.dupe(u8, avatar) else null,
                    };
                    
                    const pending_msg = PendingAttachmentMessage{
                        .chat_id = message_chat_id,
                        .user_info = cloned_user,
                        .content = message_content.?,
                    };
                    
                    try self.pending_images.put(file_id, pending_msg);
                    
                    if (self.config.debug_mode) {
                        print("[Telegram] Stored pending image message for file_id {d}\n", .{file_id});
                    }
                } else {
                    // Image is ready or it's a text message, call the handler immediately
                    if (self.message_handler) |handler| {
                        if (self.message_handler_ctx) |ctx| {
                            handler(ctx, message_chat_id, user_info, message_content.?);
                        }
                    }
                }
            } else {
                // Request user info and cache the message for later processing
                if (self.config.debug_mode) {
                    print("[Telegram] User info not cached for {d}, requesting info and avatar...\n", .{user_id});
                }
                try self.requestUserInfo(user_id);
                try self.requestUserProfilePhotos(user_id);
                
                // For now, create a minimal user info to allow message processing
                const minimal_user = UserInfo{
                    .user_id = user_id,
                    .first_name = try std.fmt.allocPrint(self.allocator, "User{d}", .{user_id}),
                    .last_name = null,
                    .username = null,
                    .avatar_url = null,
                };
                
                // Check if we need to wait for image download
                const should_wait_for_image = switch (message_content.?) {
                    .attachment => |attachment| attachment.url == null,
                    .text_with_attachment => |text_attachment| text_attachment.attachment.url == null,
                    .text => false,
                };
                
                if (should_wait_for_image) {
                    // Store pending image message with minimal user info
                    const file_id = switch (message_content.?) {
                        .attachment => |attachment| attachment.file_id,
                        .text_with_attachment => |text_attachment| text_attachment.attachment.file_id,
                        .text => unreachable,
                    };
                    
                    const pending_msg = PendingAttachmentMessage{
                        .chat_id = message_chat_id,
                        .user_info = minimal_user,
                        .content = message_content.?,
                    };
                    
                    try self.pending_images.put(file_id, pending_msg);
                    
                    if (self.config.debug_mode) {
                        print("[Telegram] Stored pending image message with minimal user info for file_id {d}\n", .{file_id});
                    }
                } else {
                    // Text message or image is ready, call handler immediately
                    if (self.message_handler) |handler| {
                        if (self.message_handler_ctx) |ctx| {
                            handler(ctx, message_chat_id, minimal_user, message_content.?);
                        }
                    }
                    
                    // Clean up the minimal user info
                    self.allocator.free(minimal_user.first_name);
                }
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
        if (root.get("id")) |id| {
            const user_id = id.integer;
            
            // Check if this is for ourselves
            if (self.my_user_id == null) {
                self.my_user_id = user_id;
                if (self.config.debug_mode) {
                    print("[Telegram] My user ID: {d}\n", .{user_id});
                }
            }
            
            // Extract user information
            var first_name: []const u8 = "";
            var last_name: ?[]const u8 = null;
            var username: ?[]const u8 = null;
            
            if (root.get("first_name")) |fname| {
                first_name = fname.string;
            }
            
            if (root.get("last_name")) |lname| {
                if (lname != .null) {
                    last_name = lname.string;
                }
            }
            
            if (root.get("usernames")) |usernames| {
                const usernames_obj = usernames.object;
                if (usernames_obj.get("editable_username")) |editable_username| {
                    if (editable_username != .null) {
                        username = editable_username.string;
                    }
                }
            } else if (root.get("username")) |uname| {
                if (uname != .null) {
                    username = uname.string;
                }
            }
            
            // Store user info in cache
            const user_info = UserInfo{
                .user_id = user_id,
                .first_name = try self.allocator.dupe(u8, first_name),
                .last_name = if (last_name) |lname| try self.allocator.dupe(u8, lname) else null,
                .username = if (username) |uname| try self.allocator.dupe(u8, uname) else null,
                .avatar_url = null, // Will be set when we get profile photos
            };
            
            try self.user_cache.put(user_id, user_info);
            
            if (self.config.debug_mode) {
                print("[Telegram] Cached user info for {d}: {s}\n", .{ user_id, first_name });
            }
            
            if (user_id == self.my_user_id) {
                if (username) |uname| {
                    print("[Telegram] Logged in as: {s} (@{s})\n", .{ first_name, uname });
                } else {
                    print("[Telegram] Logged in as: {s}\n", .{first_name});
                }
            }
        }
    }

    fn handleUserProfilePhotos(self: *Self, root: std.json.ObjectMap) !void {
        if (self.config.debug_mode) {
            print("[Telegram] handleUserProfilePhotos called\n", .{});
        }
        
        if (root.get("@extra")) |extra| {
            const extra_str = extra.string;
            if (self.config.debug_mode) {
                print("[Telegram] Profile photos extra: {s}\n", .{extra_str});
            }
            
            if (std.mem.startsWith(u8, extra_str, "photos_")) {
                const user_id_str = extra_str[7..];
                const user_id = std.fmt.parseInt(i64, user_id_str, 10) catch |err| {
                    if (self.config.debug_mode) {
                        print("[Telegram] Failed to parse user ID from extra: {s}, error: {}\n", .{ user_id_str, err });
                    }
                    return;
                };
                
                if (self.config.debug_mode) {
                    print("[Telegram] Processing profile photos for user {d}\n", .{user_id});
                }
                
                if (root.get("photos")) |photos| {
                    const photos_array = photos.array;
                    if (self.config.debug_mode) {
                        print("[Telegram] Found {d} photos for user {d}\n", .{ photos_array.items.len, user_id });
                    }
                    
                    if (photos_array.items.len > 0) {
                        const first_photo = photos_array.items[0].object;
                        if (self.config.debug_mode) {
                            print("[Telegram] Processing first photo for user {d}\n", .{user_id});
                        }
                        
                        if (first_photo.get("sizes")) |sizes| {
                            const sizes_array = sizes.array;
                            if (self.config.debug_mode) {
                                print("[Telegram] Found {d} photo sizes\n", .{sizes_array.items.len});
                            }
                            
                            if (sizes_array.items.len > 0) {
                                // Get the largest size
                                const largest_size = sizes_array.items[sizes_array.items.len - 1].object;
                                if (self.config.debug_mode) {
                                    print("[Telegram] Processing largest size (index {d})\n", .{sizes_array.items.len - 1});
                                }
                                
                                if (largest_size.get("photo")) |photo| {
                                    const photo_obj = photo.object;
                                    if (self.config.debug_mode) {
                                        print("[Telegram] Found photo object\n", .{});
                                    }
                                    
                                    if (photo_obj.get("remote")) |remote| {
                                        const remote_obj = remote.object;
                                        if (self.config.debug_mode) {
                                            print("[Telegram] Found remote object\n", .{});
                                        }
                                        
                                        if (remote_obj.get("unique_id")) |unique_id| {
                                            print("[Telegram] Using unique_id for avatar: {s}\n", .{unique_id.string});
                                            
                                            // The t.me/i/userpic URLs don't work reliably
                                            // Instead, we need to use the actual file ID with Telegram Bot API
                                            // or download the file through TDLib
                                            
                                            // For now, let's try to construct a working URL
                                            // Option 1: Try to use the file ID directly (this requires a bot token)
                                            // Option 2: Download the file and serve it locally
                                            // Option 3: Use a placeholder or fallback
                                            
                                            print("[Telegram] ⚠️  Telegram profile pictures require special handling\n", .{});
                                            print("[Telegram] File ID: {s}\n", .{remote_obj.get("id").?.string});
                                            print("[Telegram] Unique ID: {s}\n", .{unique_id.string});
                                            
                                            // For now, let's not set an avatar URL since the t.me URLs don't work
                                            // This will make Discord use the default avatar
                                            print("[Telegram] ❌ Skipping avatar URL - Telegram profile pics need bot token or file download\n", .{});
                                            
                                            // TODO: Implement one of these solutions:
                                            // 1. Use Telegram Bot API with bot token to get file URL
                                            // 2. Download file through TDLib and serve locally
                                            // 3. Use a different avatar service
                                        }
                                        
                                        // Also check for other potential ID fields
                                        if (remote_obj.get("id")) |id| {
                                            print("[Telegram] Remote ID: {s}\n", .{id.string});
                                        }
                                        if (remote_obj.get("url")) |url| {
                                            print("[Telegram] Remote URL: {s}\n", .{url.string});
                                        }
                                    }
                                }
                            } else {
                                if (self.config.debug_mode) {
                                    print("[Telegram] ❌ No sizes found in photo\n", .{});
                                }
                            }
                        } else {
                            if (self.config.debug_mode) {
                                print("[Telegram] ❌ No sizes array found in photo\n", .{});
                            }
                        }
                    } else {
                        if (self.config.debug_mode) {
                            print("[Telegram] ❌ User {d} has no profile photos\n", .{user_id});
                        }
                    }
                } else {
                    if (self.config.debug_mode) {
                        print("[Telegram] ❌ No photos array found in response\n", .{});
                    }
                }
            } else {
                if (self.config.debug_mode) {
                    print("[Telegram] ❌ Extra string doesn't start with 'photos_': {s}\n", .{extra_str});
                }
            }
        } else {
            if (self.config.debug_mode) {
                print("[Telegram] ❌ No @extra field found in profile photos response\n", .{});
            }
        }
    }

    fn handleChatPhotos(self: *Self, root: std.json.ObjectMap) !void {
        print("[Telegram] 📸 Processing chat photos response\n", .{});
        
        if (root.get("@extra")) |extra| {
            const extra_str = extra.string;
            print("[Telegram] Chat photos extra: {s}\n", .{extra_str});
            
            if (std.mem.startsWith(u8, extra_str, "photos_")) {
                const user_id_str = extra_str[7..];
                const user_id = std.fmt.parseInt(i64, user_id_str, 10) catch |err| {
                    print("[Telegram] Failed to parse user ID from extra: {s}, error: {}\n", .{ user_id_str, err });
                    return;
                };
                
                print("[Telegram] Processing chat photos for user {d}\n", .{user_id});
                
                if (root.get("photos")) |photos| {
                    const photos_array = photos.array;
                    print("[Telegram] Found {d} chat photos for user {d}\n", .{ photos_array.items.len, user_id });
                    
                    if (photos_array.items.len > 0) {
                        // Get the first photo
                        const first_photo = photos_array.items[0].object;
                        if (first_photo.get("sizes")) |sizes| {
                            const sizes_array = sizes.array;
                            if (sizes_array.items.len > 0) {
                                // Get the largest size
                                const largest_size = sizes_array.items[sizes_array.items.len - 1].object;
                                
                                if (largest_size.get("photo")) |photo| {
                                    const photo_obj = photo.object;
                                    
                                    if (photo_obj.get("id")) |file_id_value| {
                                        const file_id = @as(i32, @intCast(file_id_value.integer));
                                        print("[Telegram] 📥 Starting download for file_id {d} (user {d})\n", .{ file_id, user_id });
                                        
                                        // Get unique_id for identification
                                        var unique_id_str: []const u8 = "unknown";
                                        if (photo_obj.get("remote")) |remote| {
                                            const remote_obj = remote.object;
                                            if (remote_obj.get("unique_id")) |unique_id| {
                                                unique_id_str = unique_id.string;
                                            }
                                        }
                                        
                                        // Create extra data to identify this download
                                        const download_extra = try std.fmt.allocPrint(
                                            self.allocator,
                                            "avatar_{d}_{d}_{s}",
                                            .{ user_id, file_id, unique_id_str }
                                        );
                                        defer self.allocator.free(download_extra);
                                        
                                        // Download the file with high priority (32 = high priority)
                                        try self.downloadFile(file_id, 32, download_extra);
                                        
                                        print("[Telegram] 📤 Requested download for avatar file_id {d}\n", .{file_id});
                                    } else {
                                        print("[Telegram] ❌ No file ID found in photo object\n", .{});
                                    }
                                }
                            }
                        }
                    } else {
                        print("[Telegram] ❌ User {d} has no chat photos\n", .{user_id});
                    }
                } else {
                    print("[Telegram] ❌ No photos array found in chatPhotos response\n", .{});
                }
            } else {
                print("[Telegram] ❌ Extra string doesn't start with 'photos_': {s}\n", .{extra_str});
            }
        } else {
            print("[Telegram] ❌ No @extra field found in chatPhotos response\n", .{});
        }
    }

    fn handleFile(self: *Self, root: std.json.ObjectMap) !void {
        if (self.config.debug_mode) {
            print("[Telegram] Processing file download response\n", .{});
        }
        
        if (root.get("@extra")) |extra| {
            const extra_str = extra.string;
            if (self.config.debug_mode) {
                print("[Telegram] File extra: {s}\n", .{extra_str});
            }
            
            // Check if this is an avatar download
            if (std.mem.startsWith(u8, extra_str, "avatar_")) {
                // Parse user_id and file_id from extra string: "avatar_{user_id}_{file_id}_{unique_id}"
                var parts = std.mem.splitScalar(u8, extra_str, '_');
                _ = parts.next(); // skip "avatar"
                if (parts.next()) |user_id_str| {
                    if (parts.next()) |file_id_str| {
                        const user_id = std.fmt.parseInt(i64, user_id_str, 10) catch |err| {
                            print("[Telegram] Failed to parse user ID from file extra: {s}, error: {}\n", .{ user_id_str, err });
                            return;
                        };
                        
                                                const file_id = std.fmt.parseInt(i32, file_id_str, 10) catch |err| {
                            print("[Telegram] Failed to parse file ID from avatar extra: {s}, error: {}\n", .{ file_id_str, err });
                            return;
                        };
                        
                        if (self.config.debug_mode) {
                            print("[Telegram] Processing avatar download for user {d}, file {d}\n", .{ user_id, file_id });
                        }
                        
                        // Check if file was downloaded successfully
                        if (root.get("local")) |local| {
                            const local_obj = local.object;
                            
                            if (local_obj.get("is_downloading_completed")) |is_completed| {
                                if (is_completed.bool) {
                                    if (local_obj.get("path")) |path| {
                                        const file_path = path.string;
                                        if (self.config.debug_mode) {
                                            print("[Telegram] Avatar downloaded successfully: {s}\n", .{file_path});
                                        }
                                        
                                        // Store file ID to path mapping for HTTP server lookup
                                        const stored_path = try self.allocator.dupe(u8, file_path);
                                        try self.file_id_to_path.put(file_id, stored_path);
                                        
                                        // Update user cache with avatar URL using file ID
                                        if (self.user_cache.getPtr(user_id)) |user_info| {
                                            // Free old avatar URL if it exists
                                            if (user_info.avatar_url) |old_url| {
                                                self.allocator.free(old_url);
                                            }
                                            
                                            // Generate URL using file ID instead of filename to prevent collisions
                                            const avatar_url = try self.generateFileUrl(file_id, file_path);
                                            
                                            // Set HTTP URL as avatar URL
                                            user_info.avatar_url = avatar_url;
                                            
                                            if (self.config.debug_mode) {
                                                print("[Telegram] Avatar URL set for user {d}: {s}\n", .{ user_id, avatar_url });
                                                print("[Telegram] Local file path: {s}\n", .{file_path});
                                            }
                                        } else {
                                            print("[Telegram] ⚠️  User {d} not found in cache when setting avatar\n", .{user_id});
                                        }
                                    } else {
                                        print("[Telegram] ❌ No path found in downloaded file\n", .{});
                                    }
                                } else {
                                    print("[Telegram] ⏳ Avatar download still in progress for user {d}\n", .{user_id});
                                }
                            } else {
                                print("[Telegram] ❌ No download completion status found\n", .{});
                            }
                        } else {
                            print("[Telegram] ❌ No local file info found\n", .{});
                        }
                    } else {
                        print("[Telegram] ❌ Failed to parse file ID from avatar extra: {s}\n", .{extra_str});
                    }
                } else {
                    print("[Telegram] ❌ Failed to parse user ID from avatar extra: {s}\n", .{extra_str});
                }
            } else if (std.mem.startsWith(u8, extra_str, "attachment_")) {
                // Parse attachment download: "attachment_{chat_id}_{user_id}_{file_id}"
                var parts = std.mem.splitScalar(u8, extra_str, '_');
                _ = parts.next(); // skip "attachment"
                
                if (parts.next()) |chat_id_str| {
                    if (parts.next()) |user_id_str| {
                        if (parts.next()) |file_id_str| {
                            const chat_id = std.fmt.parseInt(i64, chat_id_str, 10) catch |err| {
                                print("[Telegram] Failed to parse chat ID from attachment extra: {s}, error: {}\n", .{ chat_id_str, err });
                                return;
                            };
                            
                            const user_id = std.fmt.parseInt(i64, user_id_str, 10) catch |err| {
                                print("[Telegram] Failed to parse user ID from attachment extra: {s}, error: {}\n", .{ user_id_str, err });
                                return;
                            };
                            
                            const file_id = std.fmt.parseInt(i32, file_id_str, 10) catch |err| {
                                print("[Telegram] Failed to parse file ID from attachment extra: {s}, error: {}\n", .{ file_id_str, err });
                                return;
                            };
                            
                            if (self.config.debug_mode) {
                                print("[Telegram] Processing attachment download for chat {d}, user {d}, file {d}\n", .{ chat_id, user_id, file_id });
                            }
                            
                            // Check if file was downloaded successfully
                            if (root.get("local")) |local| {
                                const local_obj = local.object;
                                
                                if (local_obj.get("is_downloading_completed")) |is_completed| {
                                    if (is_completed.bool) {
                                        if (local_obj.get("path")) |path| {
                                            const file_path = path.string;
                                            print("[Telegram] 📁 Attachment downloaded successfully: {s}\n", .{file_path});
                                            
                                            // Store file ID to path mapping for HTTP server lookup
                                            const stored_path = try self.allocator.dupe(u8, file_path);
                                            try self.file_id_to_path.put(file_id, stored_path);
                                            
                                            // Generate URL using file ID instead of filename to prevent collisions
                                            const attachment_url = try self.generateFileUrl(file_id, file_path);
                                            defer self.allocator.free(attachment_url);
                                            
                                            print("[Telegram] 🌐 Attachment available at: {s}\n", .{attachment_url});
                                            
                                            // Check if we have a pending message for this file
                                            if (self.pending_images.getPtr(file_id)) |pending_msg| {
                                                // Update the attachment URL in the pending message
                                                switch (pending_msg.content) {
                                                    .attachment => |*attachment| {
                                                        if (attachment.url) |old_url| {
                                                            self.allocator.free(old_url);
                                                        }
                                                        attachment.url = try self.allocator.dupe(u8, attachment_url);
                                                    },
                                                    .text_with_attachment => |*text_attachment| {
                                                        if (text_attachment.attachment.url) |old_url| {
                                                            self.allocator.free(old_url);
                                                        }
                                                        text_attachment.attachment.url = try self.allocator.dupe(u8, attachment_url);
                                                    },
                                                    .text => unreachable,
                                                }
                                                
                                                // Send the message now that the attachment is ready
                                                if (self.message_handler) |handler| {
                                                    if (self.message_handler_ctx) |ctx| {
                                                        handler(ctx, pending_msg.chat_id, pending_msg.user_info, pending_msg.content);
                                                    }
                                                }
                                                
                                                // Remove from pending messages
                                                if (self.pending_images.fetchRemove(file_id)) |entry| {
                                                    var mutable_entry = entry;
                                                    mutable_entry.value.deinit(self.allocator);
                                                }
                                                
                                                if (self.config.debug_mode) {
                                                    print("[Telegram] ✅ Sent pending attachment message to Discord\n", .{});
                                                }
                                            } else {
                                                if (self.config.debug_mode) {
                                                    print("[Telegram] ⚠️  No pending message found for file_id {d}\n", .{file_id});
                                                }
                                            }
                                        } else {
                                            print("[Telegram] ❌ No path found in downloaded attachment file\n", .{});
                                        }
                                    } else {
                                        print("[Telegram] ⏳ Attachment download still in progress for file {d}\n", .{file_id});
                                    }
                                } else {
                                    print("[Telegram] ❌ No download completion status found for attachment\n", .{});
                                }
                            } else {
                                print("[Telegram] ❌ No local file info found for attachment\n", .{});
                            }
                        }
                    }
                }
            } else {
                if (self.config.debug_mode) {
                    print("[Telegram] File download not related to avatar or attachment: {s}\n", .{extra_str});
                }
            }
        } else {
            if (self.config.debug_mode) {
                print("[Telegram] File response has no @extra field\n", .{});
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

    pub fn sendMessage(self: *Self, chat_id: i64, text: []const u8) !void {
        // Build the JSON request manually to avoid complex nested struct issues
        const request = try std.fmt.allocPrint(self.allocator,
            "{{\"@type\":\"sendMessage\",\"chat_id\":{d},\"input_message_content\":{{\"@type\":\"inputMessageText\",\"text\":{{\"@type\":\"formattedText\",\"text\":\"{s}\"}}}}}}",
            .{ chat_id, text }
        );
        defer self.allocator.free(request);
        
        try self.send(request);
        
        if (self.config.debug_mode) {
            print("[Telegram] Sent message to chat {d}: {s}\n", .{ chat_id, text });
        }
    }

    pub fn tick(self: *Self) !bool {
        if (self.is_closed) return false;
        
        if (self.receive(self.config.receive_timeout)) |result| {
            try self.processUpdate(result);
        }
        
        return true;
    }

    // Helper function to determine the correct HTTP endpoint based on file type
    fn getFileEndpoint(self: *Self, file_path: []const u8) []const u8 {
        _ = self;
        
        // Check file extension to determine endpoint
        if (std.mem.endsWith(u8, file_path, ".jpg") or 
           std.mem.endsWith(u8, file_path, ".jpeg") or 
           std.mem.endsWith(u8, file_path, ".png") or 
           std.mem.endsWith(u8, file_path, ".gif") or 
           std.mem.endsWith(u8, file_path, ".webp") or 
           std.mem.endsWith(u8, file_path, ".bmp") or 
           std.mem.endsWith(u8, file_path, ".svg")) {
            return "avatar"; // Images go to /avatar/ endpoint
        } else if (std.mem.endsWith(u8, file_path, ".mp4") or 
                  std.mem.endsWith(u8, file_path, ".webm") or 
                  std.mem.endsWith(u8, file_path, ".avi") or 
                  std.mem.endsWith(u8, file_path, ".mov") or 
                  std.mem.endsWith(u8, file_path, ".mp3") or 
                  std.mem.endsWith(u8, file_path, ".ogg") or 
                  std.mem.endsWith(u8, file_path, ".wav") or 
                  std.mem.endsWith(u8, file_path, ".flac")) {
            return "file"; // Media files go to /file/ endpoint
        } else {
            return "files"; // Everything else goes to /files/ endpoint
        }
    }

    // Helper function to get file extension from path
    fn getFileExtension(self: *Self, file_path: []const u8) []const u8 {
        _ = self;
        if (std.mem.lastIndexOfScalar(u8, file_path, '.')) |dot_index| {
            return file_path[dot_index..];
        }
        return "";
    }

    // Helper function to generate URL based on file ID instead of filename
    fn generateFileUrl(self: *Self, file_id: i32, file_path: []const u8) ![]const u8 {
        // Get file extension to preserve MIME type detection
        const extension = self.getFileExtension(file_path);
        
        // Determine endpoint based on file type
        const endpoint = self.getFileEndpoint(file_path);
        
        // Generate URL using file ID instead of filename to prevent collisions
        const url_filename = try std.fmt.allocPrint(
            self.allocator,
            "{d}{s}",
            .{ file_id, extension }
        );
        defer self.allocator.free(url_filename);
        
        // Create HTTP URL for the file server
        const url = try std.fmt.allocPrint(
            self.allocator,
            "{s}/{s}/{s}",
            .{ self.config.avatar_base_url, endpoint, url_filename }
        );
        
        print("[Telegram] 🔗 Generated URL for file_id {d}: {s}\n", .{ file_id, url });
        
        return url;
    }

    // Helper function to extract caption from message content
    fn extractCaption(self: *Self, content_obj: std.json.ObjectMap) ?[]const u8 {
        _ = self;
        if (content_obj.get("caption")) |caption_obj| {
            const caption_text_obj = caption_obj.object;
            if (caption_text_obj.get("text")) |caption_text| {
                return caption_text.string;
            }
        }
        return null;
    }

    // Helper function to start file download with appropriate prefix
    fn startFileDownload(self: *Self, file_id: i32, prefix: []const u8, chat_id: i64, user_id: i64) !void {
        const download_extra = try std.fmt.allocPrint(
            self.allocator,
            "{s}_{d}_{d}_{d}",
            .{ prefix, chat_id, user_id, file_id }
        );
        defer self.allocator.free(download_extra);
        
        try self.downloadFile(file_id, 32, download_extra);
        
        if (self.config.debug_mode) {
            print("[Telegram] 📥 Started download for {s} file_id {d}\n", .{ prefix, file_id });
        }
    }

    fn parsePhotoMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 📸 Processing photo message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("photo")) |photo| {
            const photo_obj = photo.object;
            
            // Get the largest photo size
            if (photo_obj.get("sizes")) |sizes| {
                const sizes_array = sizes.array;
                if (sizes_array.items.len > 0) {
                    // Get the largest size (last in array)
                    const largest_size = sizes_array.items[sizes_array.items.len - 1].object;
                    
                    if (largest_size.get("photo")) |size_photo| {
                        const size_photo_obj = size_photo.object;
                        
                        var file_id: i32 = 0;
                        var width: i32 = 0;
                        var height: i32 = 0;
                        
                        if (size_photo_obj.get("id")) |id| {
                            file_id = @as(i32, @intCast(id.integer));
                        }
                        
                        if (largest_size.get("width")) |w| {
                            width = @as(i32, @intCast(w.integer));
                        }
                        
                        if (largest_size.get("height")) |h| {
                            height = @as(i32, @intCast(h.integer));
                        }
                        
                        const attachment_info = AttachmentInfo{
                            .file_id = file_id,
                            .attachment_type = .photo,
                            .width = width,
                            .height = height,
                            .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                            .local_path = null,
                            .url = null,
                        };
                        
                        try self.startFileDownload(file_id, "attachment", chat_id, user_id);
                        
                        if (caption) |cap| {
                            return MessageContent{ .text_with_attachment = .{
                                .text = cap,
                                .attachment = attachment_info,
                            }};
                        } else {
                            return MessageContent{ .attachment = attachment_info };
                        }
                    }
                }
            }
        }
        return null;
    }

    fn parseDocumentMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 📄 Processing document message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("document")) |document| {
            const document_obj = document.object;
            
            var file_id: i32 = 0;
            var file_name: ?[]const u8 = null;
            var mime_type: ?[]const u8 = null;
            var file_size: i64 = 0;
            
            if (document_obj.get("document")) |doc_file| {
                const doc_file_obj = doc_file.object;
                
                if (doc_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (doc_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (document_obj.get("file_name")) |fname| {
                file_name = fname.string;
            }
            
            if (document_obj.get("mime_type")) |mime| {
                mime_type = mime.string;
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .document,
                .file_size = file_size,
                .file_name = if (file_name) |name| try self.allocator.dupe(u8, name) else null,
                .mime_type = if (mime_type) |mime| try self.allocator.dupe(u8, mime) else null,
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }

    fn parseVideoMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 🎥 Processing video message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("video")) |video| {
            const video_obj = video.object;
            
            var file_id: i32 = 0;
            var width: i32 = 0;
            var height: i32 = 0;
            var duration: i32 = 0;
            var file_size: i64 = 0;
            var file_name: ?[]const u8 = null;
            var mime_type: ?[]const u8 = null;
            
            if (video_obj.get("video")) |video_file| {
                const video_file_obj = video_file.object;
                
                if (video_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (video_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (video_obj.get("width")) |w| {
                width = @as(i32, @intCast(w.integer));
            }
            
            if (video_obj.get("height")) |h| {
                height = @as(i32, @intCast(h.integer));
            }
            
            if (video_obj.get("duration")) |d| {
                duration = @as(i32, @intCast(d.integer));
            }
            
            if (video_obj.get("file_name")) |fname| {
                file_name = fname.string;
            }
            
            if (video_obj.get("mime_type")) |mime| {
                mime_type = mime.string;
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .video,
                .width = width,
                .height = height,
                .duration = duration,
                .file_size = file_size,
                .file_name = if (file_name) |name| try self.allocator.dupe(u8, name) else null,
                .mime_type = if (mime_type) |mime| try self.allocator.dupe(u8, mime) else null,
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }

    fn parseAudioMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 🎵 Processing audio message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("audio")) |audio| {
            const audio_obj = audio.object;
            
            var file_id: i32 = 0;
            var duration: i32 = 0;
            var file_size: i64 = 0;
            var file_name: ?[]const u8 = null;
            var mime_type: ?[]const u8 = null;
            
            if (audio_obj.get("audio")) |audio_file| {
                const audio_file_obj = audio_file.object;
                
                if (audio_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (audio_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (audio_obj.get("duration")) |d| {
                duration = @as(i32, @intCast(d.integer));
            }
            
            if (audio_obj.get("file_name")) |fname| {
                file_name = fname.string;
            }
            
            if (audio_obj.get("mime_type")) |mime| {
                mime_type = mime.string;
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .audio,
                .duration = duration,
                .file_size = file_size,
                .file_name = if (file_name) |name| try self.allocator.dupe(u8, name) else null,
                .mime_type = if (mime_type) |mime| try self.allocator.dupe(u8, mime) else null,
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }

    fn parseVoiceMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 🎤 Processing voice message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("voice_note")) |voice| {
            const voice_obj = voice.object;
            
            var file_id: i32 = 0;
            var duration: i32 = 0;
            var file_size: i64 = 0;
            
            if (voice_obj.get("voice")) |voice_file| {
                const voice_file_obj = voice_file.object;
                
                if (voice_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (voice_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (voice_obj.get("duration")) |d| {
                duration = @as(i32, @intCast(d.integer));
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .voice,
                .duration = duration,
                .file_size = file_size,
                .mime_type = try self.allocator.dupe(u8, "audio/ogg"),
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }

    fn parseVideoNoteMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 📹 Processing video note message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("video_note")) |video_note| {
            const video_note_obj = video_note.object;
            
            var file_id: i32 = 0;
            var duration: i32 = 0;
            var file_size: i64 = 0;
            var length: i32 = 0;
            
            if (video_note_obj.get("video")) |video_file| {
                const video_file_obj = video_file.object;
                
                if (video_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (video_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (video_note_obj.get("duration")) |d| {
                duration = @as(i32, @intCast(d.integer));
            }
            
            if (video_note_obj.get("length")) |l| {
                length = @as(i32, @intCast(l.integer));
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .video_note,
                .width = length,
                .height = length,
                .duration = duration,
                .file_size = file_size,
                .mime_type = try self.allocator.dupe(u8, "video/mp4"),
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }

    fn parseStickerMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 🎭 Processing sticker message\n", .{});
        }
        
        message_text.* = "";
        
        if (content_obj.get("sticker")) |sticker| {
            const sticker_obj = sticker.object;
            
            var file_id: i32 = 0;
            var width: i32 = 0;
            var height: i32 = 0;
            var file_size: i64 = 0;
            
            if (sticker_obj.get("sticker")) |sticker_file| {
                const sticker_file_obj = sticker_file.object;
                
                if (sticker_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (sticker_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (sticker_obj.get("width")) |w| {
                width = @as(i32, @intCast(w.integer));
            }
            
            if (sticker_obj.get("height")) |h| {
                height = @as(i32, @intCast(h.integer));
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .sticker,
                .width = width,
                .height = height,
                .file_size = file_size,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            return MessageContent{ .attachment = attachment_info };
        }
        return null;
    }

    fn parseAnimationMessage(self: *Self, content_obj: std.json.ObjectMap, message_text: *[]const u8, chat_id: i64, user_id: i64) !?MessageContent {
        if (self.config.debug_mode) {
            print("[Telegram] 🎞️ Processing animation message\n", .{});
        }
        
        const caption = self.extractCaption(content_obj);
        message_text.* = caption orelse "";
        
        if (content_obj.get("animation")) |animation| {
            const animation_obj = animation.object;
            
            var file_id: i32 = 0;
            var width: i32 = 0;
            var height: i32 = 0;
            var duration: i32 = 0;
            var file_size: i64 = 0;
            var file_name: ?[]const u8 = null;
            var mime_type: ?[]const u8 = null;
            
            if (animation_obj.get("animation")) |animation_file| {
                const animation_file_obj = animation_file.object;
                
                if (animation_file_obj.get("id")) |id| {
                    file_id = @as(i32, @intCast(id.integer));
                }
                
                if (animation_file_obj.get("size")) |size| {
                    file_size = size.integer;
                }
            }
            
            if (animation_obj.get("width")) |w| {
                width = @as(i32, @intCast(w.integer));
            }
            
            if (animation_obj.get("height")) |h| {
                height = @as(i32, @intCast(h.integer));
            }
            
            if (animation_obj.get("duration")) |d| {
                duration = @as(i32, @intCast(d.integer));
            }
            
            if (animation_obj.get("file_name")) |fname| {
                file_name = fname.string;
            }
            
            if (animation_obj.get("mime_type")) |mime| {
                mime_type = mime.string;
            }
            
            const attachment_info = AttachmentInfo{
                .file_id = file_id,
                .attachment_type = .animation,
                .width = width,
                .height = height,
                .duration = duration,
                .file_size = file_size,
                .file_name = if (file_name) |name| try self.allocator.dupe(u8, name) else null,
                .mime_type = if (mime_type) |mime| try self.allocator.dupe(u8, mime) else null,
                .caption = if (caption) |cap| try self.allocator.dupe(u8, cap) else null,
                .local_path = null,
                .url = null,
            };
            
            try self.startFileDownload(file_id, "attachment", chat_id, user_id);
            
            if (caption) |cap| {
                return MessageContent{ .text_with_attachment = .{
                    .text = cap,
                    .attachment = attachment_info,
                }};
            } else {
                return MessageContent{ .attachment = attachment_info };
            }
        }
        return null;
    }


}; 