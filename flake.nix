{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs supportedSystems (system: f system);
      nixpkgsFor = forAllSystems (
        system:
        import nixpkgs {
          inherit system;
        }
      );

      buildDkm =
        {
          pkgs,
          targetPkgs ? pkgs,
          goos ? null,
          goarch ? null,
        }:
        let
          systemConfig = {
            "x86_64-linux" = {
              os = "linux";
              arch = "amd64";
            };
            "aarch64-linux" = {
              os = "linux";
              arch = "arm64";
            };
          };

          targetSystem = targetPkgs.stdenv.hostPlatform.system;
          targetConfig = systemConfig.${targetSystem} or (throw "Unsupported target system: ${targetSystem}");

          finalGoos = if goos != null then goos else targetConfig.os;
          finalGoarch = if goarch != null then goarch else targetConfig.arch;

          buildGoModuleCross =
            if pkgs.stdenv.hostPlatform.system != targetPkgs.stdenv.hostPlatform.system then
              pkgs.buildGoModule
            else
              targetPkgs.buildGoModule;

        in
        buildGoModuleCross {
          name = "dkm";
          src = ./.;

          vendorHash = "sha256-9smxGxt+XHXc6KZnGxCQ9SlFGPu7BmsLATV/O4fybFU=";

          nativeBuildInputs = [ pkgs.go_1_22 ];
          buildInputs = [ ];

          stdenv = targetPkgs.stdenv;
          CGO_ENABLED = "1";
          GOOS = finalGoos;
          GOARCH = finalGoarch;
          CC = "${targetPkgs.stdenv.cc}/bin/${targetPkgs.stdenv.cc.targetPrefix}cc";
          CGO_CFLAGS = "-O2";
          CGO_LDFLAGS = "";

          buildPhase = ''
            export CC="${targetPkgs.stdenv.cc}/bin/${targetPkgs.stdenv.cc.targetPrefix}cc"
            export CGO_ENABLED=1
            export GOOS=${finalGoos}
            export GOARCH=${finalGoarch}
            make
          '';

          installPhase = ''
            mkdir -p $out/bin
            cp dkm $out/bin/
          '';

          meta = with pkgs.lib; {
            description = "Doge Key Manager";
            homepage = "https://github.com/dogeorg/dkm";
            license = licenses.mit;
            maintainers = with maintainers; [ dogecoinfoundation ];
            platforms = platforms.all;
          };
        };

      crossPkgsFor =
        buildSystem: targetSystem:
        if buildSystem == targetSystem then
          nixpkgsFor.${buildSystem}
        else if buildSystem == "x86_64-linux" && targetSystem == "aarch64-linux" then
          nixpkgsFor.${buildSystem}.pkgsCross.aarch64-multiplatform
        else if buildSystem == "aarch64-linux" && targetSystem == "x86_64-linux" then
          nixpkgsFor.${buildSystem}.pkgsCross.gnu64
        else
          throw "Cross compilation from ${buildSystem} to ${targetSystem} is not supported";

      buildDkmFor =
        buildSystem: targetSystem:
        let
          pkgs = nixpkgsFor.${buildSystem};
          targetPkgs = crossPkgsFor buildSystem targetSystem;
        in
        buildDkm { inherit pkgs targetPkgs; };

      buildDkmMatrix =
        buildSystem:
        nixpkgs.lib.genAttrs supportedSystems (
          targetSystem:
          if crossPkgsFor buildSystem targetSystem != null then buildDkmFor buildSystem targetSystem else null
        );
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = buildDkmFor system system;
          dkm = buildDkmFor system system;

          cross = nixpkgs.lib.filterAttrs (n: v: v != null) (buildDkmMatrix system);
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = [
              pkgs.go_1_22
            ];
          };
        }
      );

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-rfc-style);

      dbxSessionName = "dkm";
      dbxStartCommand = "make dev";
    };
}
