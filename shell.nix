{ pkgs ? import <nixpkgs> { } }:
(import ./.).devShells.${pkgs.system}.default
