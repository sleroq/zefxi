const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;
const c = std.c;

// TDLib C function declarations
extern "c" fn td_create_client_id() c_int;
extern "c" fn td_send(client_id: c_int, request: [*:0]const u8) void;
extern "c" fn td_receive(timeout: f64) ?[*:0]const u8;
extern "c" fn td_execute(request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_set_log_message_callback(max_verbosity_level: c_int, callback: ?*const fn (c_int, [*:0]const u8) callconv(.C) void) void;

// Alternative TDLib JSON client interface (newer)
extern "c" fn td_json_client_create() ?*anyopaque;
extern "c" fn td_json_client_send(client: *anyopaque, request: [*:0]const u8) void;
extern "c" fn td_json_client_receive(client: *anyopaque, timeout: f64) ?[*:0]const u8;
extern "c" fn td_json_client_execute(client: ?*anyopaque, request: [*:0]const u8) ?[*:0]const u8;
extern "c" fn td_json_client_destroy(client: *anyopaque) void;

const TelegramClient = struct {
    allocator: Allocator,
    api_id: i32,
    api_hash: []const u8,
    client_id: c_int,
    is_authorized: bool,
    is_closed: bool,
    
    const Self = @This();

    pub fn init(allocator: Allocator, api_id: i32, api_hash: []const u8) Self {
        return Self{
            .allocator = allocator,
            .api_id = api_id,
            .api_hash = api_hash,
            .client_id = 0,
            .is_authorized = false,
            .is_closed = false,
        };
    }

    pub fn deinit(self: *Self) void {
        _ = self;
        // TDLib client instances are destroyed automatically after they are closed
    }

    pub fn start(self: *Self) !void {
        // Set log verbosity level
        const log_request = "{\"@type\":\"setLogVerbosityLevel\",\"new_verbosity_level\":2}";
        _ = td_execute(log_request.ptr);
        
        // Create client
        self.client_id = td_create_client_id();
        print("Created TDLib client with ID: {d}\n", .{self.client_id});
        
        // Test execute method
        print("\nTesting TDLib execute method...\n", .{});
        const test_request = "{\"@type\":\"getTextEntities\",\"text\":\"@telegram /test_command https://telegram.org telegram.me\"}";
        if (td_execute(test_request.ptr)) |result| {
            const result_str = std.mem.span(result);
            print("Text entities result: {s}\n", .{result_str});
        }
        
        // Get TDLib version
        const version_request = "{\"@type\":\"getOption\",\"name\":\"version\"}";
        self.send(version_request);
    }

    pub fn send(self: *Self, request: []const u8) void {
        // Create null-terminated string
        const request_cstr = self.allocator.dupeZ(u8, request) catch {
            print("Failed to allocate memory for request\n", .{});
            return;
        };
        defer self.allocator.free(request_cstr);
        
        td_send(self.client_id, request_cstr.ptr);
        print("Sent request: {s}\n", .{request});
    }

    pub fn receive(self: *Self, timeout: f64) ?[]const u8 {
        _ = self; // Suppress unused parameter warning
        if (td_receive(timeout)) |result| {
            return std.mem.span(result);
        }
        return null;
    }

    pub fn setTdlibParameters(self: *Self) void {
        var buffer = std.ArrayList(u8).init(self.allocator);
        defer buffer.deinit();
        
        const writer = buffer.writer();
        
        // Build setTdlibParameters request
        writer.print(
            "{{\"@type\":\"setTdlibParameters\"," ++
            "\"use_test_dc\":false," ++
            "\"database_directory\":\"tdlib\"," ++
            "\"files_directory\":\"tdlib\"," ++
            "\"database_encryption_key\":\"\"," ++
            "\"use_file_database\":true," ++
            "\"use_chat_info_database\":true," ++
            "\"use_message_database\":true," ++
            "\"use_secret_chats\":true," ++
            "\"api_id\":{d}," ++
            "\"api_hash\":\"{s}\"," ++
            "\"system_language_code\":\"en\"," ++
            "\"device_model\":\"Desktop\"," ++
            "\"system_version\":\"Linux\"," ++
            "\"application_version\":\"1.0\"," ++
            "\"enable_storage_optimizer\":true," ++
            "\"ignore_file_names\":false}}", 
            .{ self.api_id, self.api_hash }
        ) catch return;
        
        self.send(buffer.items);
    }

    pub fn setAuthenticationPhoneNumber(self: *Self, phone_number: []const u8) void {
        var buffer = std.ArrayList(u8).init(self.allocator);
        defer buffer.deinit();
        
        const writer = buffer.writer();
        writer.print(
            "{{\"@type\":\"setAuthenticationPhoneNumber\",\"phone_number\":\"{s}\"}}", 
            .{phone_number}
        ) catch return;
        
        self.send(buffer.items);
    }

    pub fn checkAuthenticationCode(self: *Self, code: []const u8) void {
        var buffer = std.ArrayList(u8).init(self.allocator);
        defer buffer.deinit();
        
        const writer = buffer.writer();
        writer.print(
            "{{\"@type\":\"checkAuthenticationCode\",\"code\":\"{s}\"}}", 
            .{code}
        ) catch return;
        
        self.send(buffer.items);
    }

    pub fn checkAuthenticationPassword(self: *Self, password: []const u8) void {
        var buffer = std.ArrayList(u8).init(self.allocator);
        defer buffer.deinit();
        
        const writer = buffer.writer();
        writer.print(
            "{{\"@type\":\"checkAuthenticationPassword\",\"password\":\"{s}\"}}", 
            .{password}
        ) catch return;
        
        self.send(buffer.items);
    }

    pub fn registerUser(self: *Self, first_name: []const u8, last_name: []const u8) void {
        var buffer = std.ArrayList(u8).init(self.allocator);
        defer buffer.deinit();
        
        const writer = buffer.writer();
        writer.print(
            "{{\"@type\":\"registerUser\",\"first_name\":\"{s}\",\"last_name\":\"{s}\"}}", 
            .{ first_name, last_name }
        ) catch return;
        
        self.send(buffer.items);
    }

    pub fn processUpdate(self: *Self, update_json: []const u8) !void {
        print("Received update: {s}\n", .{update_json});
        
        // Parse JSON to determine update type
        const parsed = std.json.parseFromSlice(std.json.Value, self.allocator, update_json, .{}) catch |err| {
            print("Failed to parse JSON: {}\n", .{err});
            return;
        };
        defer parsed.deinit();
        
        const root = parsed.value.object;
        
        if (root.get("@type")) |type_value| {
            const update_type = type_value.string;
            
            // Only show important updates to reduce noise
            if (std.mem.eql(u8, update_type, "updateAuthorizationState") or
                std.mem.eql(u8, update_type, "updateNewMessage") or
                std.mem.eql(u8, update_type, "updateConnectionState") or
                std.mem.eql(u8, update_type, "chats") or
                std.mem.eql(u8, update_type, "user") or
                std.mem.eql(u8, update_type, "ok") or
                std.mem.eql(u8, update_type, "error")) {
                print("Update type: {s}\n", .{update_type});
            }
            
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
            } else if (std.mem.eql(u8, update_type, "ok")) {
                print("✅ Request completed successfully\n", .{});
            } else if (std.mem.eql(u8, update_type, "error")) {
                try self.handleError(root);
            }
        }
    }

    fn handleAuthorizationState(self: *Self, root: std.json.ObjectMap) !void {
        if (root.get("authorization_state")) |auth_state| {
            const state_obj = auth_state.object;
            if (state_obj.get("@type")) |state_type| {
                const state_name = state_type.string;
                print("Authorization state: {s}\n", .{state_name});
                
                if (std.mem.eql(u8, state_name, "authorizationStateWaitTdlibParameters")) {
                    print("Setting TDLib parameters...\n", .{});
                    self.setTdlibParameters();
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitPhoneNumber")) {
                    print("Please enter your phone number (with country code, e.g., +1234567890): ", .{});
                    const phone = try self.readInput();
                    defer self.allocator.free(phone);
                    self.setAuthenticationPhoneNumber(phone);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitCode")) {
                    print("Please enter the authentication code: ", .{});
                    const code = try self.readInput();
                    defer self.allocator.free(code);
                    self.checkAuthenticationCode(code);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitPassword")) {
                    print("Please enter your 2FA password: ", .{});
                    const password = try self.readInput();
                    defer self.allocator.free(password);
                    self.checkAuthenticationPassword(password);
                } else if (std.mem.eql(u8, state_name, "authorizationStateWaitRegistration")) {
                    print("Please enter your first name: ", .{});
                    const first_name = try self.readInput();
                    defer self.allocator.free(first_name);
                    print("Please enter your last name: ", .{});
                    const last_name = try self.readInput();
                    defer self.allocator.free(last_name);
                    self.registerUser(first_name, last_name);
                } else if (std.mem.eql(u8, state_name, "authorizationStateReady")) {
                    print("✅ Authorization successful!\n", .{});
                    self.is_authorized = true;
                    
                    // Get current user info
                    self.send("{\"@type\":\"getMe\"}");
                    
                    // Get chats to monitor for messages
                    self.send("{\"@type\":\"getChats\",\"limit\":20}");
                } else if (std.mem.eql(u8, state_name, "authorizationStateClosed")) {
                    print("TDLib client closed\n", .{});
                    self.is_closed = true;
                }
            }
        }
    }

    fn handleNewMessage(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        print("📨 NEW MESSAGE RECEIVED!\n", .{});
        
        if (root.get("message")) |message| {
            const msg_obj = message.object;
            
            // Get chat ID
            if (msg_obj.get("chat_id")) |chat_id| {
                print("  Chat ID: {d}\n", .{chat_id.integer});
            }
            
            // Get sender info
            if (msg_obj.get("sender_id")) |sender_id| {
                const sender_obj = sender_id.object;
                if (sender_obj.get("user_id")) |user_id| {
                    print("  From User ID: {d}\n", .{user_id.integer});
                }
            }
            
            // Get message content
            if (msg_obj.get("content")) |content| {
                const content_obj = content.object;
                if (content_obj.get("@type")) |content_type| {
                    print("  Content Type: {s}\n", .{content_type.string});
                }
                
                if (content_obj.get("text")) |text| {
                    const text_obj = text.object;
                    if (text_obj.get("text")) |text_content| {
                        print("  📝 Text: {s}\n", .{text_content.string});
                    }
                }
            }
            
            print("  ─────────────────────────────\n", .{});
        }
    }

    fn handleConnectionState(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("state")) |state| {
            const state_obj = state.object;
            if (state_obj.get("@type")) |state_type| {
                print("Connection state: {s}\n", .{state_type.string});
            }
        }
    }

    fn handleOption(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("name")) |name| {
            if (root.get("value")) |value| {
                const value_obj = value.object;
                if (value_obj.get("value")) |actual_value| {
                    print("Option {s}: {s}\n", .{ name.string, actual_value.string });
                }
            }
        }
    }

    fn handleChats(self: *Self, root: std.json.ObjectMap) !void {
        print("📋 Received chat list\n", .{});
        if (root.get("chat_ids")) |chat_ids| {
            const chat_array = chat_ids.array;
            print("Found {d} chats, opening them to receive messages...\n", .{chat_array.items.len});
            
            for (chat_array.items) |chat_id_value| {
                const chat_id = chat_id_value.integer;
                
                // Open each chat to receive messages in real-time
                var buffer = std.ArrayList(u8).init(self.allocator);
                defer buffer.deinit();
                
                const writer = buffer.writer();
                writer.print("{{\"@type\":\"openChat\",\"chat_id\":{d}}}", .{chat_id}) catch continue;
                
                self.send(buffer.items);
                print("📂 Opened chat {d}\n", .{chat_id});
                
                // Small delay between opening chats
                std.time.sleep(100 * std.time.ns_per_ms);
            }
            
            print("✅ All chats opened! You should now receive messages in real-time.\n", .{});
        } else {
            print("❌ No chat_ids found in response\n", .{});
        }
    }

    fn handleUser(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("first_name")) |first_name| {
            if (root.get("username")) |username| {
                print("👤 Logged in as: {s} (@{s})\n", .{ first_name.string, username.string });
            } else {
                print("👤 Logged in as: {s}\n", .{first_name.string});
            }
        }
    }

    fn handleError(self: *Self, root: std.json.ObjectMap) !void {
        _ = self;
        if (root.get("message")) |message| {
            if (root.get("code")) |code| {
                print("❌ Error {d}: {s}\n", .{ code.integer, message.string });
            } else {
                print("❌ Error: {s}\n", .{message.string});
            }
        } else {
            print("❌ Unknown error occurred\n", .{});
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

    pub fn run(self: *Self) !void {
        const timeout = 1.0; // 1 second timeout
        
        print("\n=== Starting TDLib Event Loop ===\n", .{});
        print("Press Ctrl+C to exit\n\n", .{});
        
        while (!self.is_closed) {
            if (self.receive(timeout)) |result| {
                try self.processUpdate(result);
            }
            
            // Small delay to prevent busy waiting
            std.time.sleep(10 * std.time.ns_per_ms);
        }
    }
};

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // Get API credentials from environment variables
    const api_id_str = std.process.getEnvVarOwned(allocator, "TELEGRAM_API_ID") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_API_ID environment variable\n", .{});
            print("Get your API credentials from https://my.telegram.org/apps\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(api_id_str);

    const api_hash = std.process.getEnvVarOwned(allocator, "TELEGRAM_API_HASH") catch |err| switch (err) {
        error.EnvironmentVariableNotFound => {
            print("Error: Please set TELEGRAM_API_HASH environment variable\n", .{});
            print("Get your API credentials from https://my.telegram.org/apps\n", .{});
            return;
        },
        else => return err,
    };
    defer allocator.free(api_hash);

    const api_id = std.fmt.parseInt(i32, api_id_str, 10) catch {
        print("Error: TELEGRAM_API_ID must be a valid integer\n", .{});
        return;
    };

    print("Starting Zefxi - TDLib Telegram Client\n", .{});
    print("API ID: {d}\n", .{api_id});
    print("API Hash: {s}...\n", .{api_hash[0..@min(8, api_hash.len)]});

    var client = TelegramClient.init(allocator, api_id, api_hash);
    defer client.deinit();

    // Start the client
    try client.start();
    
    // Run the main event loop
    try client.run();

    print("\n🎉 TDLib client session completed!\n", .{});
} 