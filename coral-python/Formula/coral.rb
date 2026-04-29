class Coral < Formula
  desc "Multi-agent orchestration system for AI coding agents with a web dashboard"
  homepage "https://github.com/cdknorow/coral"
  url "https://github.com/cdknorow/coral/archive/refs/tags/v2.3.1.tar.gz"
  sha256 ""  # TODO: fill after release is published
  license "MIT"

  depends_on "go" => :build
  depends_on "tmux"

  def install
    cd "coral-go" do
      ldflags = "-s -w"
      system "go", "build", *std_go_args(ldflags:), "./cmd/coral/"

      # Also build launch-coral and coral-board utilities
      system "go", "build", *std_go_args(ldflags:, output: bin/"launch-coral"), "./cmd/launch-coral/"
      system "go", "build", *std_go_args(ldflags:, output: bin/"coral-board"), "./cmd/coral-board/"
    end
  end

  def caveats
    <<~EOS
      Coral is installed! To get started:

        # Start the web dashboard
        coral

        # Launch agents in worktrees
        launch-coral /path/to/worktrees

      Dashboard runs at http://localhost:8420 by default.
    EOS
  end

  test do
    assert_match "Coral", shell_output("#{bin}/coral --help 2>&1", 2)
  end
end
