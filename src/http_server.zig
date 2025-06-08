const std = @import("std");
const print = std.debug.print;
const Allocator = std.mem.Allocator;

pub const HttpServer = struct {
    allocator: Allocator,
    port: u16,
    files_directory: []const u8,
    debug_mode: bool,
    file_id_to_path: ?*std.HashMap(i32, []const u8, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage),
    
    const Self = @This();
    
    pub fn init(allocator: Allocator, port: u16, files_directory: []const u8, debug_mode: bool) Self {
        return Self{
            .allocator = allocator,
            .port = port,
            .files_directory = files_directory,
            .debug_mode = debug_mode,
            .file_id_to_path = null,
        };
    }
    
    pub fn setFileIdMapping(self: *Self, file_id_to_path: *std.HashMap(i32, []const u8, std.hash_map.AutoContext(i32), std.hash_map.default_max_load_percentage)) void {
        self.file_id_to_path = file_id_to_path;
    }
    
    pub fn start(self: *Self) !void {
        const address = std.net.Address.parseIp("127.0.0.1", self.port) catch unreachable;
        var net_server = try address.listen(.{ .reuse_address = true });
        defer net_server.deinit();
        
        print("[HTTP] File server listening on http://127.0.0.1:{d}\n", .{self.port});
        print("[HTTP] Serving files from: {s}\n", .{self.files_directory});
        print("[HTTP] Available endpoints: /avatar/, /file/, /files/\n", .{});
        
        while (true) {
            const connection = try net_server.accept();
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
        
        if (std.mem.startsWith(u8, target, "/files/")) {
            const filename = target[7..];
            try self.serveFile(request, filename);
        } else if (std.mem.startsWith(u8, target, "/avatar/")) {
            const filename = target[8..];
            try self.serveFile(request, filename);
        } else if (std.mem.startsWith(u8, target, "/file/")) {
            const filename = target[6..];
            try self.serveFile(request, filename);
        } else if (std.mem.eql(u8, target, "/")) {
            try self.serveIndex(request);
        } else {
            try self.serveNotFound(request);
        }
    }
    
    fn serveFile(self: *Self, request: *std.http.Server.Request, filename: []const u8) !void {
        // Security check: prevent directory traversal
        if (std.mem.indexOf(u8, filename, "..") != null or 
            std.mem.indexOf(u8, filename, "\\") != null) {
            try self.serveNotFound(request);
            return;
        }
        
        var file_path: []const u8 = undefined;
        var should_free_path = false;
        
        // Try to resolve file ID to actual path
        if (std.mem.lastIndexOfScalar(u8, filename, '.')) |dot_index| {
            const id_part = filename[0..dot_index];
            if (std.fmt.parseInt(i32, id_part, 10)) |file_id| {
                if (self.file_id_to_path) |mapping| {
                    if (mapping.get(file_id)) |actual_path| {
                        file_path = actual_path;
                        if (self.debug_mode) {
                            print("[HTTP] Resolved file ID {d} to path: {s}\n", .{ file_id, actual_path });
                        }
                    } else {
                        if (self.debug_mode) {
                            print("[HTTP] File ID {d} not found in mapping\n", .{file_id});
                        }
                        try self.serveNotFound(request);
                        return;
                    }
                } else {
                    if (self.debug_mode) {
                        print("[HTTP] No file ID mapping available\n", .{});
                    }
                    try self.serveNotFound(request);
                    return;
                }
            } else |_| {
                // Not a file ID, treat as regular filename
                file_path = try std.fmt.allocPrint(
                    self.allocator,
                    "{s}/{s}",
                    .{ self.files_directory, filename }
                );
                should_free_path = true;
            }
        } else {
            // No extension, treat as regular filename
            file_path = try std.fmt.allocPrint(
                self.allocator,
                "{s}/{s}",
                .{ self.files_directory, filename }
            );
            should_free_path = true;
        }
        
        if (should_free_path) {
            defer self.allocator.free(file_path);
        }
        
        if (self.debug_mode) {
            print("[HTTP] Serving file: {s}\n", .{file_path});
        }
        
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
        
        const content_type = self.getContentType(filename);
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
        const html_template = 
            \\<!DOCTYPE html>
            \\<html>
            \\<head>
            \\    <title>Zefxi File Server</title>
            \\    <style>
            \\        body { font-family: Arial, sans-serif; margin: 40px; }
            \\        .container { max-width: 800px; margin: 0 auto; }
            \\        .header { text-align: center; margin-bottom: 40px; }
            \\        .info { background: #f5f5f5; padding: 20px; border-radius: 8px; }
            \\        code { background: #e8e8e8; padding: 2px 6px; border-radius: 4px; }
            \\        .url-examples { margin: 10px 0; }
            \\    </style>
            \\</head>
            \\<body>
            \\    <div class="container">
            \\        <div class="header">
            \\            <h1>📁 Zefxi File Server</h1>
            \\            <p>Local HTTP server for serving Telegram files</p>
            \\        </div>
            \\        <div class="info">
            \\            <h3>📋 Usage</h3>
            \\            <p>Files are served through multiple endpoints:</p>
            \\            <div class="url-examples">
            \\                <p><code>http://127.0.0.1:{PORT}/avatar/FILENAME</code> - For images/avatars</p>
            \\                <p><code>http://127.0.0.1:{PORT}/file/FILENAME</code> - For media files</p>
            \\                <p><code>http://127.0.0.1:{PORT}/files/FILENAME</code> - For documents/other files</p>
            \\            </div>
            \\            <br>
            \\            <h3>📁 Files Directory</h3>
            \\            <p><code>{FILES_DIR}</code></p>
            \\            <br>
            \\            <h3>🎯 Supported File Types</h3>
            \\            <p>📸 Images: JPEG, PNG, GIF, WebP, SVG, BMP</p>
            \\            <p>🎥 Videos: MP4, WebM, AVI, MOV</p>
            \\            <p>🎵 Audio: MP3, OGG, WAV, FLAC</p>
            \\            <p>📄 Documents: PDF, DOC, TXT, etc.</p>
            \\            <br>
            \\            <h3>🔧 Status</h3>
            \\            <p>✅ Server is running and ready to serve files</p>
            \\        </div>
            \\    </div>
            \\</body>
            \\</html>
        ;
        
        const port_str = try std.fmt.allocPrint(self.allocator, "{d}", .{self.port});
        defer self.allocator.free(port_str);
        
        const html_with_port = try std.mem.replaceOwned(u8, self.allocator, html_template, "{PORT}", port_str);
        defer self.allocator.free(html_with_port);
        
        const final_html = try std.mem.replaceOwned(u8, self.allocator, html_with_port, "{FILES_DIR}", self.files_directory);
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
    
    fn getContentType(self: *Self, filename: []const u8) []const u8 {
        _ = self;
        
        // Images
        if (std.mem.endsWith(u8, filename, ".jpg") or std.mem.endsWith(u8, filename, ".jpeg")) {
            return "image/jpeg";
        } else if (std.mem.endsWith(u8, filename, ".png")) {
            return "image/png";
        } else if (std.mem.endsWith(u8, filename, ".gif")) {
            return "image/gif";
        } else if (std.mem.endsWith(u8, filename, ".webp")) {
            return "image/webp";
        } else if (std.mem.endsWith(u8, filename, ".svg")) {
            return "image/svg+xml";
        } else if (std.mem.endsWith(u8, filename, ".bmp")) {
            return "image/bmp";
        } else if (std.mem.endsWith(u8, filename, ".ico")) {
            return "image/x-icon";
        }
        
        // Videos
        else if (std.mem.endsWith(u8, filename, ".mp4")) {
            return "video/mp4";
        } else if (std.mem.endsWith(u8, filename, ".webm")) {
            return "video/webm";
        } else if (std.mem.endsWith(u8, filename, ".avi")) {
            return "video/x-msvideo";
        } else if (std.mem.endsWith(u8, filename, ".mov")) {
            return "video/quicktime";
        }
        
        // Audio
        else if (std.mem.endsWith(u8, filename, ".mp3")) {
            return "audio/mpeg";
        } else if (std.mem.endsWith(u8, filename, ".ogg")) {
            return "audio/ogg";
        } else if (std.mem.endsWith(u8, filename, ".wav")) {
            return "audio/wav";
        } else if (std.mem.endsWith(u8, filename, ".flac")) {
            return "audio/flac";
        } else if (std.mem.endsWith(u8, filename, ".aac")) {
            return "audio/aac";
        } else if (std.mem.endsWith(u8, filename, ".m4a")) {
            return "audio/mp4";
        } else if (std.mem.endsWith(u8, filename, ".opus")) {
            return "audio/opus";
        }
        
        // Documents
        else if (std.mem.endsWith(u8, filename, ".pdf")) {
            return "application/pdf";
        } else if (std.mem.endsWith(u8, filename, ".doc")) {
            return "application/msword";
        } else if (std.mem.endsWith(u8, filename, ".docx")) {
            return "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
        } else if (std.mem.endsWith(u8, filename, ".txt")) {
            return "text/plain";
        } else if (std.mem.endsWith(u8, filename, ".html") or std.mem.endsWith(u8, filename, ".htm")) {
            return "text/html";
        } else if (std.mem.endsWith(u8, filename, ".css")) {
            return "text/css";
        } else if (std.mem.endsWith(u8, filename, ".js")) {
            return "application/javascript";
        } else if (std.mem.endsWith(u8, filename, ".json")) {
            return "application/json";
        } else if (std.mem.endsWith(u8, filename, ".xml")) {
            return "application/xml";
        }
        
        // Archives
        else if (std.mem.endsWith(u8, filename, ".zip")) {
            return "application/zip";
        } else if (std.mem.endsWith(u8, filename, ".rar")) {
            return "application/vnd.rar";
        } else if (std.mem.endsWith(u8, filename, ".7z")) {
            return "application/x-7z-compressed";
        } else if (std.mem.endsWith(u8, filename, ".tar")) {
            return "application/x-tar";
        } else if (std.mem.endsWith(u8, filename, ".gz")) {
            return "application/gzip";
        }
        
        // Default
        else {
            return "application/octet-stream";
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
            \\    <p><a href="/">← Back to File Server</a></p>
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