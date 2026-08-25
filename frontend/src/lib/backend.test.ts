import { afterEach, describe, expect, test } from "bun:test";
import { openDirectoryDialog, openFileDialog } from "./backend";

const originalWindow = globalThis.window;

afterEach(() => {
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    writable: true,
    value: originalWindow,
  });
});

describe("native dialog bridge", () => {
  test("opens a directory picker through the Go app binding", async () => {
    const titles: string[] = [];
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      writable: true,
      value: {
        go: {
          app: {
            App: {
              OpenDirectoryDialog: async (title: string) => {
                titles.push(title);
                return "C:/workspace";
              },
            },
          },
        },
      },
    });

    await expect(openDirectoryDialog("Choose repository folder")).resolves.toBe("C:/workspace");
    expect(titles).toEqual(["Choose repository folder"]);
  });

  test("opens a multi-file picker through the Go app binding", async () => {
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      writable: true,
      value: {
        go: {
          app: {
            App: {
              OpenMultipleFilesDialog: async () => ["C:/one.txt", "C:/two.txt"],
            },
          },
        },
      },
    });

    await expect(openFileDialog("Attach files")).resolves.toEqual(["C:/one.txt", "C:/two.txt"]);
  });
});
