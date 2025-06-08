const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;

pub const HttpServer = struct {
    allocator: Allocator,
    port: u16,
    files_directory: []const u8,
    debug_mode: bool,
    
    const Self = @This();
    
    pub fn init(allocator: Allocator, port: u16, files_directory: []const u8, debug_mode: bool) Self {
        return Self{
            .allocator = allocator,
            .port = port,
            .files_directory = files_directory,
            .debug_mode = debug_mode,
        };
    }
    
    pub fn start(self: *Self) !void {
        const address = std.net.Address.parseIp("127.0.0.1", self.port) catch unreachable;
        var net_server = try address.listen(.{ .reuse_address = true });
        defer net_server.deinit();
        
        print("[HTTP] Avatar server listening on http://127.0.0.1:{d}\n", .{self.port});
        print("[HTTP] Serving files from: {s}\n", .{self.files_directory});
        
        while (true) {
            const connection = try net_server.accept();
            
            // Handle each connection in a separate thread for better performance
            const thread = try std.Thread.spawn(.{}, handleConnection, .{ self, connection });
            thread.detach();
        }
    }
    
    fn handleConnection(self: *Self, connection: std.net.Server.Connection) void {
        defer connection.stream.close();
        
        var read_buffer: [4096]u8 = undefined;
        var server = std.http.Server.init(connection, &read_buffer);
        
        while (true) {
            var request = server.receiveHead() catch |err| switch (err) {
                error.HttpConnectionClosing => break,
                else => {
                    if (self.debug_mode) {
                        print("[HTTP] Error receiving request: {}\n", .{err});
                    }
                    break;
                },
            };
            
            self.handleRequest(&request) catch |err| {
                if (self.debug_mode) {
                    print("[HTTP] Error handling request: {}\n", .{err});
                }
                break;
            };
        }
    }
    
    fn handleRequest(self: *Self, request: *std.http.Server.Request) !void {
        const target = request.head.target;
        if (self.debug_mode) {
            print("[HTTP] Request: {s}\n", .{target});
        }
        
        // Parse the request path
        if (std.mem.startsWith(u8, target, "/avatar/")) {
            const filename = target[8..]; // Remove "/avatar/" prefix
            try self.serveFile(request, filename);
        } else if (std.mem.eql(u8, target, "/")) {
            try self.serveIndex(request);
        } else {
            try self.serveNotFound(request);
        }
    }
    
    fn serveFile(self: *Self, request: *std.http.Server.Request, filename: []const u8) !void {
        // Validate filename to prevent directory traversal
        if (std.mem.indexOf(u8, filename, "..") != null or 
            std.mem.indexOf(u8, filename, "/") != null or
            std.mem.indexOf(u8, filename, "\\") != null) {
            try self.serveNotFound(request);
            return;
        }
        
        // Construct full file path
        const file_path = try std.fmt.allocPrint(
            self.allocator,
            "{s}/{s}",
            .{ self.files_directory, filename }
        );
        defer self.allocator.free(file_path);
        
        if (self.debug_mode) {
            print("[HTTP] Serving file: {s}\n", .{file_path});
        }
        
        // Try to open and read the file
        const file = std.fs.cwd().openFile(file_path, .{}) catch |err| {
            if (self.debug_mode) {
                print("[HTTP] File not found: {s} (error: {})\n", .{ file_path, err });
            }
            try self.serveNotFound(request);
            return;
        };
        defer file.close();
        
        const file_size = try file.getEndPos();
        const file_content = try self.allocator.alloc(u8, file_size);
        defer self.allocator.free(file_content);
        
        _ = try file.readAll(file_content);
        
        // Determine content type based on file extension
        const content_type = if (std.mem.endsWith(u8, filename, ".jpg") or std.mem.endsWith(u8, filename, ".jpeg"))
            "image/jpeg"
        else if (std.mem.endsWith(u8, filename, ".png"))
            "image/png"
        else if (std.mem.endsWith(u8, filename, ".gif"))
            "image/gif"
        else if (std.mem.endsWith(u8, filename, ".webp"))
            "image/webp"
        else
            "application/octet-stream";
        
        // Send response
        try request.respond(file_content, .{
            .status = .ok,
            .extra_headers = &.{
                .{ .name = "content-type", .value = content_type },
                .{ .name = "cache-control", .value = "public, max-age=3600" },
            },
        });
        
        if (self.debug_mode) {
            print("[HTTP] Served {s} ({d} bytes, {s})\n", .{ filename, file_size, content_type });
        }
    }
    
    fn serveIndex(self: *Self, request: *std.http.Server.Request) !void {
        const html = 
            \\<!DOCTYPE html>
            \\<html>
            \\<head>
            \\    <title>Zefxi Avatar Server</title>
            \\    <style>
            \\        body { font-family: Arial, sans-serif; margin: 40px; }
            \\        .container { max-width: 800px; margin: 0 auto; }
            \\        .header { text-align: center; margin-bottom: 40px; }
            \\        .info { background: #f5f5f5; padding: 20px; border-radius: 8px; }
            \\        code { background: #e8e8e8; padding: 2px 6px; border-radius: 4px; }
            \\    </style>
            \\</head>
            \\<body>
            \\    <div class="container">
            \\        <div class="header">
            \\            <h1>🖼️ Zefxi Avatar Server</h1>
            \\            <p>Local HTTP server for serving Telegram avatar images</p>
            \\        </div>
            \\        <div class="info">
            \\            <h3>📋 Usage</h3>
            \\            <p>Access avatar files using the following URL pattern:</p>
            \\            <p><code>http://127.0.0.1:PORT/avatar/FILENAME</code></p>
            \\            <br>
            \\            <h3>📁 Files Directory</h3>
            \\            <p><code>FILES_DIR</code></p>
            \\            <br>
            \\            <h3>🔧 Status</h3>
            \\            <p>✅ Server is running and ready to serve avatar images</p>
            \\        </div>
            \\    </div>
            \\</body>
            \\</html>
        ;
        
        // Replace placeholders
        const html_with_port = try std.mem.replaceOwned(u8, self.allocator, html, "PORT", try std.fmt.allocPrint(self.allocator, "{d}", .{self.port}));
        defer self.allocator.free(html_with_port);
        
        const final_html = try std.mem.replaceOwned(u8, self.allocator, html_with_port, "FILES_DIR", self.files_directory);
        defer self.allocator.free(final_html);
        
        try request.respond(final_html, .{
            .status = .ok,
            .extra_headers = &.{
                .{ .name = "content-type", .value = "text/html; charset=utf-8" },
            },
        });
        
        if (self.debug_mode) {
            print("[HTTP] Served index page\n", .{});
        }
    }
    
    fn serveNotFound(self: *Self, request: *std.http.Server.Request) !void {
        const html = 
            \\<!DOCTYPE html>
            \\<html>
            \\<head><title>404 Not Found</title></head>
            \\<body>
            \\    <h1>404 Not Found</h1>
            \\    <p>The requested file was not found.</p>
            \\    <p><a href="/">← Back to Avatar Server</a></p>
            \\</body>
            \\</html>
        ;
        
        try request.respond(html, .{
            .status = .not_found,
            .extra_headers = &.{
                .{ .name = "content-type", .value = "text/html; charset=utf-8" },
            },
        });
        
        if (self.debug_mode) {
            print("[HTTP] Served 404 Not Found\n", .{});
        }
    }
}; 