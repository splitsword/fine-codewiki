class Codewiki < Formula
  desc "CLI tool to turn any codebase into an interactive learning wiki"
  homepage "https://github.com/splitsword/fine-codewiki"
  version "0.1.0"
  license "MIT"

  depends_on "go" => :build

  if OS.mac? && Hardware::CPU.arm?
    url "https://github.com/splitsword/fine-codewiki/releases/download/v#{version}/codewiki-v#{version}-darwin-arm64.tar.gz"
  elsif OS.mac? && Hardware::CPU.intel?
    url "https://github.com/splitsword/fine-codewiki/releases/download/v#{version}/codewiki-v#{version}-darwin-amd64.tar.gz"
  elsif OS.linux? && Hardware::CPU.intel?
    url "https://github.com/splitsword/fine-codewiki/releases/download/v#{version}/codewiki-v#{version}-linux-amd64.tar.gz"
  elsif OS.linux? && Hardware::CPU.arm?
    url "https://github.com/splitsword/fine-codewiki/releases/download/v#{version}/codewiki-v#{version}-linux-arm64.tar.gz"
  end

  def install
    bin.install "codewiki"
  end

  test do
    system "#{bin}/codewiki", "--version"
  end
end
