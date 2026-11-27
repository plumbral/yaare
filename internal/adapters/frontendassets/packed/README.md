Generated desktop frontend archive lives here as frontend.zip.
Run bun run build:web to rebuild it. It is not a source file or user data.
This tracked placeholder keeps Go compilation and binding generation usable
before the first web build; production startup requires the generated archive.
