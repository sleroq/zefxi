const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const exe = b.addExecutable(.{
        .name = "zefxi",
        .root_source_file = b.path("src/main.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
        .use_llvm = true,
    });

    // Link TDLib JSON library
    exe.linkSystemLibrary("tdjson");

    // Add discord.zig dependency
    const discord_dep = b.dependency("discordzig", .{});
    exe.root_module.addImport("discord", discord_dep.module("discord.zig"));

    b.installArtifact(exe);

    const run_cmd = b.addRunArtifact(exe);
    run_cmd.step.dependOn(b.getInstallStep());

    if (b.args) |args| {
        run_cmd.addArgs(args);
    }

    const run_step = b.step("run", "Run the app");
    run_step.dependOn(&run_cmd.step);

    const unit_tests = b.addTest(.{
        .root_source_file = b.path("src/main.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
    });

    // Link TDLib for tests too
    unit_tests.linkSystemLibrary("tdjson");
    unit_tests.root_module.addImport("discord", discord_dep.module("discord.zig"));

    const run_unit_tests = b.addRunArtifact(unit_tests);

    const test_step = b.step("test", "Run unit tests");
    test_step.dependOn(&run_unit_tests.step);
} 