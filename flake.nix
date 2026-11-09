{
  description = "Zefxi Telegram-Discord bridge";

  inputs.nixpkgs.url = "https://github.com/NixOS/nixpkgs/archive/nixpkgs-unstable.tar.gz";

  outputs =
    { self, nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.buildGo127Module {
            pname = "zefxi";
            version = "0.1.0";
            src = nixpkgs.lib.fileset.toSource {
              root = ./.;
              fileset = nixpkgs.lib.fileset.unions [
                ./go.mod
                ./go.sum
                ./cmd
                ./internal
              ];
            };
            vendorHash = "sha256-fTQijc4I1k+VL5OXmjNIMeDIaUcMIDcl5S/4HaZlYN8=";
            subPackages = [ "cmd/zefxi" ];
            env.CGO_ENABLED = "1";
            nativeBuildInputs = with pkgs; [
              makeWrapper
              ffmpeg-headless
              lottieconverter
            ];
            postInstall = ''
              wrapProgram "$out/bin/zefxi" \
                --prefix PATH : ${
                  pkgs.lib.makeBinPath [
                    pkgs.ffmpeg-headless
                    pkgs.lottieconverter
                  ]
                }
            '';
          };

          container = pkgs.dockerTools.buildLayeredImage {
            name = "zefxi";
            tag = "latest";
            contents = [ self.packages.${system}.default ];
            extraCommands = ''
              mkdir -p tmp
              chmod 1777 tmp
            '';
            config = {
              Entrypoint = [ "${self.packages.${system}.default}/bin/zefxi" ];
              Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
            };
          };
        }
      );

      devShells = forAllSystems (system: {
        default =
          let
            pkgs = nixpkgs.legacyPackages.${system};
          in
          pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              gopls
              golangci-lint
              sqlite
              ffmpeg-headless
              lottieconverter
            ];
            CGO_ENABLED = "1";
            shellHook = ''
              if [ -f .env ]; then
                source .env
              fi
            '';
          };
      });
    };
}
