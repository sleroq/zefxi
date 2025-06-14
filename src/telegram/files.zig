// Telegram TDLib File Handling
const std = @import("std");
const Allocator = std.mem.Allocator;

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

/// AttachmentInfo owns all its heap-allocated fields and must be deinitialized with deinit().
pub const AttachmentInfo = struct {
    file_id: i64,
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

    /// Frees all heap-allocated fields.
    pub fn deinit(self: *AttachmentInfo, allocator: Allocator) void {
        if (self.file_name) |fname| allocator.free(fname);
        if (self.mime_type) |mime| allocator.free(mime);
        if (self.caption) |cap| allocator.free(cap);
        if (self.local_path) |path| allocator.free(path);
        if (self.url) |url| allocator.free(url);
    }

    /// Deep copy this AttachmentInfo, duplicating all heap-allocated fields.
    pub fn deepCopy(self: AttachmentInfo, allocator: Allocator) !AttachmentInfo {
        return AttachmentInfo{
            .file_id = self.file_id,
            .attachment_type = self.attachment_type,
            .width = self.width,
            .height = self.height,
            .duration = self.duration,
            .file_size = self.file_size,
            .file_name = if (self.file_name) |fname| try allocator.dupe(u8, fname) else null,
            .mime_type = if (self.mime_type) |mime| try allocator.dupe(u8, mime) else null,
            .caption = if (self.caption) |cap| try allocator.dupe(u8, cap) else null,
            .local_path = if (self.local_path) |path| try allocator.dupe(u8, path) else null,
            .url = if (self.url) |url| try allocator.dupe(u8, url) else null,
        };
    }
};
// (Add file download/upload logic here if needed)
