{
  pkgs ? import <nixpkgs> {
    config.allowUnfree = true;
    config.android_sdk.accept_license = true;
  },
  mkGoEnv ? pkgs.mkGoEnv,
  gomod2nix ? pkgs.gomod2nix,
}: let
  goEnv = mkGoEnv {pwd = ./.;};
in
  pkgs.mkShell {
    packages = with pkgs; [
      goEnv
      gomod2nix
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
      aapt2
    ];
    shellHook = ''
      echo "Entering the development environment"
      echo "Go version: $(go version)"
      echo "Node.js version: $(node --version)"
      echo "Java version: $(java -version)"

      export ANDROID_NDK="${pkgs.androidPkgs.ndk-bundle}/share/android-ndk"
      export ANDROID_HOME="${pkgs.androidPkgs.androidsdk}/libexec/android-sdk"
      export ANDROID_NDK_ROOT="${pkgs.androidPkgs.ndk-bundle}/share/android-sdk/ndk/25.2.9519653"
      export ANDROID_SDK_ROOT="$ANDROID_HOME"
      export PATH="$ANDROID_SDK_ROOT/tools:$PATH"
      export PATH="$ANDROID_SDK_ROOT/tools/bin:$PATH"
      export PATH="$ANDROID_HOME/platform-tools:$PATH"
      export PATH="$ANDROID_HOME/build-tools/34.0.0:$PATH"
      export PATH="$PATH:${pkgs.aapt2}/bin"
      export AAPT2_PATH="${pkgs.aapt2}/bin/aapt2"
      export CGO_ENABLED=1
      export GOPATH="$HOME/go"
      export GOCACHE="$HOME/.cache/go-build"
      export GOMODCACHE="$GOPATH/pkg/mod"
      export JAVA_HOME="${pkgs.jdk17}"
      export GRADLE_HOME="${pkgs.gradle_8}"
      export PATH="$GRADLE_HOME/bin:$PATH"
      export PATH="$JAVA_HOME/bin:$PATH"

      GRADLE_PROPS="android/gradle.properties"
      if [ ! -f "$GRADLE_PROPS" ]; then
        cat > "$GRADLE_PROPS" << EOF
      org.gradle.jvmargs=-Xmx2048m -XX:MaxMetaspaceSize=512m
      android.useAndroidX=true
      android.enableJetifier=true
      android.suppressUnsupportedCompileSdk=34
      org.gradle.daemon=true
      org.gradle.parallel=true
      org.gradle.configureondemand=true
      android.aapt2.log=true
      org.gradle.logging.level=info
      EOF
      fi
      sed -i '/android.aapt2FromMavenOverride=/d' "$GRADLE_PROPS"
      sed -i '/org.gradle.java.home=/d' "$GRADLE_PROPS"
      echo "android.aapt2FromMavenOverride=${pkgs.aapt2}/bin/aapt2" >> "$GRADLE_PROPS"
      echo "org.gradle.java.home=${pkgs.jdk17}" >> "$GRADLE_PROPS"

      # Добавляем директорию для временных файлов
      export TMPDIR="/tmp"

      # Настройка для Android SDK
      mkdir -p $HOME/.android
      touch $HOME/.android/repositories.cfg

      # Настройка для Gradle
      mkdir -p $HOME/.gradle

      # Проверяем наличие необходимых инструментов
      command -v go >/dev/null 2>&1 || { echo "Go is not installed"; exit 1; }
      command -v java >/dev/null 2>&1 || { echo "Java is not installed"; exit 1; }
      command -v gradle >/dev/null 2>&1 || { echo "Gradle is not installed"; exit 1; }

      echo "Development environment is ready!"
    '';

    # Добавляем переменные среды, которые должны быть доступны в оболочке
    NIX_SHELL_PRESERVE_PROMPT = 1;

    # Добавляем библиотеки, необходимые для сборки
    LD_LIBRARY_PATH = with pkgs; lib.makeLibraryPath [
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
    ];
  }
