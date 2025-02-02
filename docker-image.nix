{
  pkgs,
  system ? builtins.currentSystem,
  buildGoApplication,
}: let
  project = import ./default.nix {inherit pkgs buildGoApplication;};
  myEnv = pkgs.buildEnv {
    name = "web-app-env";
    paths = [
      project.full-app
      pkgs.openssl
      pkgs.zlib
      pkgs.stdenv.cc.cc.lib
      pkgs.bash
      pkgs.coreutils
      pkgs.cacert  # Добавляем CA сертификаты
    ];
  };
  finalImage = pkgs.dockerTools.buildImage {
    name = "web-app";
    tag = "latest";
    architecture = "x86_64";
    copyToRoot = pkgs.buildEnv {
      name = "root";
      paths = [
        myEnv
        (pkgs.runCommand "ssl-certs" {} ''
          mkdir -p $out/etc/ssl/certs
          cp -r ${pkgs.cacert}/etc/ssl/certs/. $out/etc/ssl/certs/
        '')
      ];
    };
    config = {
      Cmd = ["${project.full-app}/bin/full-app"];
      WorkingDir = "/";
      Env = [
        "LD_LIBRARY_PATH=${pkgs.openssl.out}/lib:${pkgs.zlib}/lib:${pkgs.stdenv.cc.cc.lib}/lib"
        "STATIC_FILES_PATH=${project.full-app}/share/web/static"
        "MIGRATIONS_PATH=${project.full-app}/share/web/migrations"
        "HTTPS_CERT=${project.full-app}/share/web/certs/server.crt"
        "HTTPS_KEY=${project.full-app}/share/web/certs/server.key"
        "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"  # Добавляем переменную окружения для CA сертификатов
      ];
      ExposedPorts = {"3000/tcp" = {};};
    };
  };
in
  finalImage
