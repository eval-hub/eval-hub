class EvalhubMcp < Formula
    desc "EvalHub MCP Server - Model Context Protocol server for AI evaluation"
    homepage "https://github.com/eval-hub/eval-hub"
    version "0.4.2"
    license "Apache-2.0"

    on_macos do
      on_intel do
        url "https://github.com/eval-hub/eval-hub/releases/download/v#{version}/evalhub-mcp-darwin-amd64"
        sha256 "PLACEHOLDER"
      end
      on_arm do
        url "https://github.com/eval-hub/eval-hub/releases/download/v#{version}/evalhub-mcp-darwin-arm64"
        sha256 "PLACEHOLDER"
      end
    end

    on_linux do
      on_intel do
        url "https://github.com/eval-hub/eval-hub/releases/download/v#{version}/evalhub-mcp-linux-amd64"
        sha256 "PLACEHOLDER"
      end
      on_arm do
        url "https://github.com/eval-hub/eval-hub/releases/download/v#{version}/evalhub-mcp-linux-arm64"
        sha256 "PLACEHOLDER"
      end
    end

    def install
      binary_name = "evalhub-mcp-#{OS.mac? ? "darwin" : "linux"}-#{Hardware::CPU.intel? ? "amd64" : "arm64"}"
      bin.install binary_name => "evalhub-mcp"
    end

    test do
      assert_match "evalhub-mcp version", shell_output("#{bin}/evalhub-mcp --version")
    end
  end
