import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { markdownToText, safeUrl } from "@/lib/markdown";
import { renderWithProviders } from "@/test/render";

import { Markdown } from "./markdown";

const render = (text: string) => {
  renderWithProviders(<Markdown text={text} />);
  return screen.getByTestId("markdown");
};

describe("Markdown", () => {
  it("renders headings, emphasis, lists, links and tables", () => {
    const md = render(
      "# Upgrade\n\nThis **bumps** IBC.\n\n- one\n- two\n\nSee [the notes](https://github.com/bze-alphateam/bze).\n\n| a | b |\n|---|---|\n| 1 | 2 |",
    );
    expect(within(md).getByRole("heading", { name: "Upgrade" })).toBeInTheDocument();
    expect(within(md).getByText("bumps").tagName).toBe("STRONG");
    expect(within(md).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["one", "two"]);
    const link = within(md).getByRole("link", { name: "the notes" });
    expect(link).toHaveAttribute("href", "https://github.com/bze-alphateam/bze");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer nofollow");
    expect(within(md).getByRole("table")).toHaveTextContent("ab12");
  });

  it("never turns raw HTML into elements", () => {
    const md = render(
      'Hi <script>window.pwned = true</script><img src=x onerror="window.pwned = true"><iframe src="https://evil.example"></iframe><b onclick="x()">bold</b>',
    );
    expect(md.querySelector("script, iframe, img, b, [onerror], [onclick]")).toBeNull();
    expect((window as { pwned?: boolean }).pwned).toBeUndefined();
    expect(md).toHaveTextContent("Hi");
  });

  it("drops links that are not http(s) or mailto", () => {
    const md = render(
      "[a](javascript:alert(1)) [b](JaVaScRiPt:alert(1)) [c](data:text/html;base64,PHNjcmlwdD4=) [d](vbscript:x) [e](/relative) [f](mailto:team@getbze.com)",
    );
    const links = within(md).getAllByRole("link");
    expect(links.map((l) => l.getAttribute("href"))).toEqual(["mailto:team@getbze.com"]);
    expect(md).toHaveTextContent("a b c d e f");
  });

  it("shows an image as a link, loading nothing", () => {
    const md = render("![chart](https://tracker.example/pixel.png) ![x](javascript:alert(1))");
    expect(md.querySelector("img")).toBeNull();
    expect(within(md).getByRole("link", { name: "chart" })).toHaveAttribute("href", "https://tracker.example/pixel.png");
    expect(within(md).getAllByRole("link")).toHaveLength(1);
  });
});

describe("markdown helpers", () => {
  it("keeps only http(s) and mailto URLs", () => {
    expect(safeUrl("https://getbze.com")).toBe("https://getbze.com");
    expect(safeUrl(" http://a.example/x ")).toBe("http://a.example/x");
    expect(safeUrl("mailto:a@b.c")).toBe("mailto:a@b.c");
    for (const bad of ["javascript:alert(1)", "data:text/html,x", "/relative", "//evil.example", "https://a.example/<x>"]) {
      expect(safeUrl(bad)).toBe("");
    }
  });

  it("flattens Markdown to the words of an excerpt", () => {
    expect(
      markdownToText(
        "# Title\n\nSome **bold** and _italic_ `code`, a [link](https://x.example) and ![img](https://y.example/i.png).\n\n- item\n1. first\n> quote\n\n<b>html</b>",
      ),
    ).toBe("Title Some bold and italic code, a link and img. item first quote html");
  });
});
