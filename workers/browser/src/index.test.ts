import { describe, it, expect } from "vitest";
import { renderPage } from "./index.js";

describe("browser worker", () => {
  it("rejects invalid URLs", async () => {
    await expect(renderPage("not-a-url")).rejects.toThrow();
  });
});
