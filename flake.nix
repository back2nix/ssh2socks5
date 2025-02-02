{
  description = "SSH to SOCKS5 proxy with Android support";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
    nixpkgs-unstable.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs = { self, nixpkgs, nixpkgs-unstable, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs-unstable = import nixpkgs-unstable {
          inherit system;
          config.allowUnfree = true;
          config.android_sdk.accept_license = true;
        };
        overlays = final: prev: {
          unstable = pkgs-unstable;
          androidPkgs = prev.androidenv.composeAndroidPackages {
            toolsVersion = "26.1.1";
            platformToolsVersion = "33.0.3";
            buildToolsVersions = ["33.0.1" "34.0.0"];
            platformVersions = ["33" "34"];
            ndkVersions = [ "25.2.9519653" ];
            includeNDK = true;
            includeExtras = [ "extras;android;m2repository" ];
            includeSources = true;
          };
          gomobile = (prev.gomobile.overrideAttrs (old: {
            patches = [
              (final.fetchurl {
                url = "https://github.com/golang/mobile/commit/f20e966e05b8f7e06bed500fa0da81cf6ebca307.patch";
                sha256 = "sha256-TZ/Yhe8gMRQUZFAs9G5/cf2b9QGtTHRSObBFD5Pbh7Y=";
              })
              (final.fetchurl {
                url = "https://github.com/golang/mobile/commit/406ed3a7b8e44dc32844953647b49696d8847d51.patch";
                sha256 = "sha256-dqbYukHkQEw8npOkKykOAzMC3ot/Y4DEuh7fE+ptlr8=";
              })
            ];
          })).override {
            withAndroidPkgs = true;
            androidPkgs = final.androidPkgs;
            buildGoModule = args: prev.buildGo122Module (args // {
              go = prev.go_1_22;
            });
          };
        };
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ overlays ];
          config = {
            allowUnfree = true;
            android_sdk.accept_license = true;
          };
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_22
            jdk17
            gradle_8
            androidPkgs.platform-tools
            androidPkgs.build-tools
            androidPkgs.platforms
            unstable.gomobile
            clang

            which
            file
            procps
            findutils
            gnutar
            gzip
            gnumake
            gcc

            zlib
            libGL
            libGLU
            xorg.libX11
            xorg.libXext
            xorg.libXrender
            xorg.libXrandr
            xorg.libXi
            xorg.libXcursor
            xorg.libXfixes

            pkgsi686Linux.glibc
            pkgsi686Linux.zlib
          ];
          shellHook = ''
            export ANDROID_NDK="${pkgs.androidPkgs.ndk-bundle}/share/android-ndk"
            export ANDROID_HOME="${pkgs.androidPkgs.androidsdk}/libexec/android-sdk"
            export ANDROID_NDK_ROOT="${pkgs.androidPkgs.ndk-bundle}/share/android-sdk/ndk/25.2.9519653"
            export ANDROID_SDK_ROOT="$ANDROID_HOME"
            export PATH="$ANDROID_SDK_ROOT/tools:$PATH"
            export PATH="$ANDROID_SDK_ROOT/tools/bin:$PATH"
            export PATH="$ANDROID_HOME/platform-tools:$PATH"
            export PATH="$ANDROID_HOME/build-tools/33.0.0:$PATH"
            export CGO_ENABLED=1
            export GOPATH="$HOME/go"
            export GOCACHE="$HOME/.cache/go-build"
            export GOMODCACHE="$GOPATH/pkg/mod"
            export JAVA_HOME="${pkgs.jdk17}"
            export GRADLE_HOME="${pkgs.gradle_8}"
            export PATH="$GRADLE_HOME/bin:$PATH"
            rm -rf $HOME/.gradle/caches/
            rm -rf .gradle
          '';
        };
      });
}
