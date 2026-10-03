import { describe, it, expect } from "vitest";
import { isPrivateIP, hostnameIsPrivateIP, parseJob, redactURL, htmlToMarkdown, validateURL } from "./index.js";

describe("isPrivateIP", () => {
  it("blocks loopback", () => {
    expect(isPrivateIP("127.0.0.1")).toBe(true);
    expect(isPrivateIP("127.255.255.255")).toBe(true);
  });

  it("blocks RFC1918 10.x", () => {
    expect(isPrivateIP("10.0.0.1")).toBe(true);
    expect(isPrivateIP("10.255.255.255")).toBe(true);
  });

  it("blocks RFC1918 172.16-31.x", () => {
    expect(isPrivateIP("172.16.0.1")).toBe(true);
    expect(isPrivateIP("172.31.255.255")).toBe(true);
  });

  it("allows 172.32.x (not private)", () => {
    expect(isPrivateIP("172.32.0.1")).toBe(false);
  });

  it("blocks RFC1918 192.168.x", () => {
    expect(isPrivateIP("192.168.0.1")).toBe(true);
    expect(isPrivateIP("192.168.255.255")).toBe(true);
  });

  it("blocks link-local 169.254.x", () => {
    expect(isPrivateIP("169.254.169.254")).toBe(true);
  });

  it("blocks multicast and reserved", () => {
    expect(isPrivateIP("224.0.0.1")).toBe(true);
    expect(isPrivateIP("255.255.255.255")).toBe(true);
  });

  it("blocks RFC 6598 CGNAT (100.64.0.0/10)", () => {
    expect(isPrivateIP("100.64.0.1")).toBe(true);
    expect(isPrivateIP("100.127.255.255")).toBe(true);
  });

  it("allows 100.128.x (outside CGNAT range)", () => {
    expect(isPrivateIP("100.128.0.1")).toBe(false);
  });

  it("blocks 0.0.0.0", () => {
    expect(isPrivateIP("0.0.0.0")).toBe(true);
  });

  it("allows public IPs", () => {
    expect(isPrivateIP("8.8.8.8")).toBe(false);
    expect(isPrivateIP("1.1.1.1")).toBe(false);
    expect(isPrivateIP("93.184.216.34")).toBe(false);
  });

  it("rejects malformed addresses", () => {
    expect(isPrivateIP("not-an-ip")).toBe(true);
    expect(isPrivateIP("")).toBe(true);
  });

  it("blocks IPv6 loopback", () => {
    expect(isPrivateIP("::1")).toBe(true);
    expect(isPrivateIP("::")).toBe(true);
  });

  it("blocks IPv6 link-local", () => {
    expect(isPrivateIP("fe80::1")).toBe(true);
  });

  it("blocks IPv6 ULA", () => {
    expect(isPrivateIP("fc00::1")).toBe(true);
    expect(isPrivateIP("fd00::1")).toBe(true);
  });

  it("blocks IPv4-mapped IPv6 private", () => {
    expect(isPrivateIP("::ffff:127.0.0.1")).toBe(true);
    expect(isPrivateIP("::ffff:10.0.0.1")).toBe(true);
    expect(isPrivateIP("::ffff:169.254.169.254")).toBe(true);
  });

  it("allows IPv4-mapped IPv6 public", () => {
    expect(isPrivateIP("::ffff:93.184.216.34")).toBe(false);
  });

  it("allows public IPv6", () => {
    expect(isPrivateIP("2001:db8::1")).toBe(false);
  });

  it("blocks non-canonical IPv6 loopback (expanded form)", () => {
    expect(isPrivateIP("0:0:0:0:0:0:0:1")).toBe(true);
  });

  it("blocks non-canonical IPv4-mapped IPv6 (expanded form)", () => {
    expect(isPrivateIP("0:0:0:0:0:ffff:7f00:1")).toBe(true);
  });

  it("allows unrecognized public IPv6", () => {
    expect(isPrivateIP("::2")).toBe(false);
  });
});

describe("hostnameIsPrivateIP", () => {
  it("detects bracketed IPv6 loopback", () => {
    expect(hostnameIsPrivateIP("[::1]")).toBe(true);
  });

  it("detects bracketed IPv4-mapped private", () => {
    expect(hostnameIsPrivateIP("[::ffff:7f00:1]")).toBe(true);
  });

  it("allows plain hostname", () => {
    expect(hostnameIsPrivateIP("example.com")).toBe(false);
  });

  it("detects private IPv4", () => {
    expect(hostnameIsPrivateIP("192.168.1.1")).toBe(true);
  });

  it("allows public IPv4", () => {
    expect(hostnameIsPrivateIP("93.184.216.34")).toBe(false);
  });
});

describe("validateURL", () => {
  it("rejects private resolved IP", async () => {
    await expect(validateURL("https://example.com", "127.0.0.1")).rejects.toThrow("SSRF");
  });

  it("rejects metadata endpoint IP", async () => {
    await expect(validateURL("https://example.com", "169.254.169.254")).rejects.toThrow("SSRF");
  });

  it("accepts public resolved IP", async () => {
    await expect(validateURL("https://example.com", "93.184.216.34")).resolves.toBeUndefined();
  });

  it("rejects invalid URL", async () => {
    await expect(validateURL("not-a-url")).rejects.toThrow();
  });

  it("rejects file:// scheme", async () => {
    await expect(validateURL("file:///etc/passwd", "93.184.216.34")).rejects.toThrow("scheme");
  });

  it("rejects data: scheme", async () => {
    await expect(validateURL("data:text/html,<h1>hi</h1>", "1.2.3.4")).rejects.toThrow("scheme");
  });

  it("requires resolved_ip", async () => {
    await expect(validateURL("https://example.com")).rejects.toThrow("resolved_ip is required");
  });

  it("rejects URL with private IPv4 hostname", async () => {
    await expect(validateURL("http://169.254.169.254/latest/meta-data/", "93.184.216.34")).rejects.toThrow("hostname");
  });

  it("rejects URL with loopback hostname", async () => {
    await expect(validateURL("http://127.0.0.1/admin", "93.184.216.34")).rejects.toThrow("hostname");
  });

  it("rejects URL with IPv6 loopback hostname", async () => {
    await expect(validateURL("http://[::1]/", "93.184.216.34")).rejects.toThrow("hostname");
  });

  it("rejects URL with IPv4-mapped IPv6 hostname", async () => {
    await expect(validateURL("http://[::ffff:127.0.0.1]/", "93.184.216.34")).rejects.toThrow("hostname");
  });
});

describe("parseJob", () => {
  it("parses valid fields", () => {
    const job = parseJob(["job_id", "abc-123", "url", "https://example.com", "format", "markdown"]);
    expect(job).toEqual({
      job_id: "abc-123",
      url: "https://example.com",
      format: "markdown",
      resolved_ip: undefined,
    });
  });

  it("returns null for missing job_id", () => {
    expect(parseJob(["url", "https://example.com"])).toBeNull();
  });

  it("returns null for missing url", () => {
    expect(parseJob(["job_id", "abc-123"])).toBeNull();
  });

  it("returns null for empty fields", () => {
    expect(parseJob([])).toBeNull();
  });

  it("includes resolved_ip when present", () => {
    const job = parseJob(["job_id", "abc", "url", "https://x.com", "resolved_ip", "1.2.3.4"]);
    expect(job?.resolved_ip).toBe("1.2.3.4");
  });
});

describe("redactURL", () => {
  it("strips query params", () => {
    expect(redactURL("https://example.com/path?token=secret&user=me")).toBe("https://example.com/path");
  });

  it("strips fragment", () => {
    expect(redactURL("https://example.com/path#section")).toBe("https://example.com/path");
  });

  it("strips userinfo", () => {
    expect(redactURL("https://admin:password@example.com/path")).toBe("https://example.com/path");
  });

  it("handles invalid URL", () => {
    expect(redactURL("not-a-url")).toBe("<invalid-url>");
  });

  it("preserves scheme and host", () => {
    expect(redactURL("https://example.com/foo/bar")).toBe("https://example.com/foo/bar");
  });
});

describe("htmlToMarkdown", () => {
  it("converts headings", () => {
    const md = htmlToMarkdown("<html><body><h1>Title</h1><h2>Sub</h2></body></html>");
    expect(md).toContain("# Title");
    expect(md).toContain("## Sub");
  });

  it("converts paragraphs", () => {
    const md = htmlToMarkdown("<p>Hello world</p>");
    expect(md).toContain("Hello world");
  });

  it("converts links", () => {
    const md = htmlToMarkdown('<a href="https://example.com">Click</a>');
    expect(md).toContain("[Click](https://example.com)");
  });

  it("converts bold and italic", () => {
    const md = htmlToMarkdown("<strong>bold</strong> and <em>italic</em>");
    expect(md).toContain("**bold**");
    expect(md).toContain("_italic_");
  });

  it("converts code blocks", () => {
    const md = htmlToMarkdown("<pre><code>let x = 1;</code></pre>");
    expect(md).toContain("```");
    expect(md).toContain("let x = 1;");
  });

  it("converts unordered lists", () => {
    const md = htmlToMarkdown("<ul><li>Alpha</li><li>Beta</li></ul>");
    expect(md).toContain("-   Alpha");
    expect(md).toContain("-   Beta");
  });
});
