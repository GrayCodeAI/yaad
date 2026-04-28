class Yaad < Formula
  desc "Model-agnostic, graph-native memory for coding agents"
  homepage "https://github.com/GrayCodeAI/yaad"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/GrayCodeAI/yaad/releases/download/v0.1.0/yaad_darwin_arm64"
      sha256 "" # filled on release
    end
    on_intel do
      url "https://github.com/GrayCodeAI/yaad/releases/download/v0.1.0/yaad_darwin_amd64"
      sha256 "" # filled on release
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/GrayCodeAI/yaad/releases/download/v0.1.0/yaad_linux_arm64"
      sha256 "" # filled on release
    end
    on_intel do
      url "https://github.com/GrayCodeAI/yaad/releases/download/v0.1.0/yaad_linux_amd64"
      sha256 "" # filled on release
    end
  end

  def install
    bin.install "yaad_#{OS.mac? ? "darwin" : "linux"}_#{Hardware::CPU.arm? ? "arm64" : "amd64"}" => "yaad"
  end

  test do
    system "#{bin}/yaad", "--version"
  end
end
