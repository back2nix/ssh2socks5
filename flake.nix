{
  description = "SSH to SOCKS5 proxy with Android support";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/df27247e6f3e636c119e2610bf12d38b5e98cc79";
    nixpkgs-unstable.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix.url = "github:nix-community/gomod2nix";
  };
  outputs = { self, nixpkgs, nixpkgs-unstable, flake-utils, gomod2nix }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs-unstable = import nixpkgs-unstable {
          inherit system;
          config.allowUnfree = true;
          config.android_sdk.accept_license = true;
        };
        overlays = final: prev: {
          unstable = pkgs-unstable;
          inherit (gomod2nix.packages.${system}) gomod2nix;
          aapt2 = final.callPackage ({ lib, stdenv, fetchurl }:
            let
              inherit (lib) getAttr;
              inherit (stdenv) isLinux isDarwin;
              pname = "aapt2";
              version = "8.1.1-10154469";
              platform = if isLinux then "linux" else
                        if isDarwin then "osx" else
                        throw "Unknown platform!";
            in stdenv.mkDerivation {
              inherit pname version;
              src = fetchurl {
                url = "https://dl.google.com/dl/android/maven2/com/android/tools/build/aapt2/${version}/aapt2-${version}-${platform}.jar";
                sha256 = getAttr platform {
                  linux = "sha256-p54GGvEfAo0yk8euVO7QTu/c3zuityZhyGdhFSV6w+E=";
                  osx = "sha256-bO4ljdUEfbuns7EyT1FKGLqNGz+0bms5XsplXvzD2T0=";
                };
              };
              nativeBuildInputs = with final; [ autoPatchelfHook makeWrapper file ];
              buildInputs = with final; [ unzip gcc-unwrapped stdenv.cc.cc.lib ];
              dontUnpack = true;
              installPhase = ''
                mkdir -p $out/{bin,lib}
                cp $src $out/lib/aapt2.jar
                ${final.unzip}/bin/unzip $out/lib/aapt2.jar aapt2 -d $out/bin
                chmod +x $out/bin/aapt2
              '';
            }
          ) {};
          androidEnvCustom = final.callPackage ({ androidenv }:
            let
              androidComposition = androidenv.composeAndroidPackages {
                toolsVersion = "26.1.1";
                platformToolsVersion = "35.0.1";
                buildToolsVersions = [ "34.0.0" ];
                includeNDK = true;
                ndkVersion = "21.3.6528147";
                platformVersions = [ "28" "29" "30" "33" "34" ];
                includeSources = false;
                includeSystemImages = false;
                systemImageTypes = [ "default" ];
                abiVersions = [ "armeabi-v7a" "arm64-v8a" ];
              };
            in {
              pkgs = androidComposition;
              compose = androidComposition;
            }
          ) {};
          androidPkgs = final.androidEnvCustom.pkgs;
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
          overlays = [ overlays gomod2nix.overlays.default ];
          config = {
            allowUnfree = true;
            android_sdk.accept_license = true;
          };
        };
        ndkVersion = "21.3.6528147";
        callPackage = pkgs.callPackage;
      in
      {
        packages = {
          ssh2socks5 = callPackage ./. { inherit (pkgs) buildGoApplication; };
          default = self.packages.${system}.ssh2socks5;
        };
        devShells.default = callPackage ./shell.nix {
          inherit (pkgs) mkGoEnv gomod2nix;
        };
      }
    );
}
