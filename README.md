# SuperVibe

SuperVibe is an open-source desktop environment for working with AI coding agents.

Built with **Go**, **Wails**, **TypeScript**, and **Bun**, SuperVibe provides a fast native desktop experience for managing projects, agents, terminals, diffs, and development workflows from one interface.

## Features

- Multi-project workspace
- AI coding agent integration
- Built-in terminal and command execution
- Project and branch management
- Diff and output views
- Native Windows application
- Fast and lightweight
- Modern dark interface
- Open source

## Installation

Download the latest version of **SuperVibe** from the [Releases](https://github.com/GumballGaming/SuperVibe/releases) page.

1. Download the latest Windows `.exe`.
2. Run SuperVibe.
3. Open a project and start working with your coding agents.

No additional installation or setup is required.

## Building From Source

If you want to contribute to SuperVibe or build it yourself:

```bash
git clone https://github.com/GumballGaming/SuperVibe.git
cd SuperVibe
```

Install the frontend dependencies:

```bash
cd frontend
bun install
cd ..
```

Run the development build:

```bash
wails dev
```

Create a production build:

```bash
wails build
```

The compiled executable will be generated in:

```text
build/bin/
```

## Contributing

Contributions are welcome.

1. Fork the repository.
2. Create a new branch.
3. Make your changes.
4. Test your changes.
5. Open a pull request.

For larger changes, consider opening an issue first to discuss the implementation.

## License

SuperVibe is free and open-source software licensed under the **MIT License**.

See [`LICENSE`](LICENSE) for details.
