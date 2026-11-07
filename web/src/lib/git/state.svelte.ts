// Bumped after a Git change works, so every list on the page reloads.
export const gitData = $state({ v: 0 });
export const reload = (): void => {
  gitData.v++;
};
