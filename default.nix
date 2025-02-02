{ pkgs ? (let
    inherit (builtins) fetchTree fromJSON readFile;
    inherit ((fromJSON (readFile ./flake.lock)).nodes) nixpkgs gomod2nix;
  in
    import (fetchTree nixpkgs.locked) {
      overlays = [(import "${fetchTree gomod2nix.locked}/overlay.nix")];
    }),
  buildGoApplication ? pkgs.buildGoApplication,
}:

buildGoApplication {
  pname = "ssh2socks5";
  version = "0.1.0";
  pwd = ./.;
  src = ./.;
  modules = ./gomod2nix.toml;
  subPackages = [ "cmd/ssh2socks5" ];
  meta = with pkgs.lib; {
    description = "SSH to SOCKS5 proxy implementation";
    homepage = "https://github.com/yourusername/ssh2socks5";
    license = licenses.mit;
    maintainers = [ ];
  };
}
