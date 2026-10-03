import { describe, it, expect } from "vitest";
import { isPrivateIP, parseJob, redactURL, htmlToMarkdown, validateURL } from "./index.js";

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
