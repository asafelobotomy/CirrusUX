// Whether the server's agent will run an action, from what it reported.
// No list (an agent older than the check) means everything is allowed, as before.
export function agentRuns(actions: string[], mismatched: boolean, action: string): boolean {
  // Upgrading CirrusUX is the way out of a mismatch, so it only needs an agent that knows it.
  if (action.startsWith('self_')) return actions.includes(action);
  if (mismatched) return false;
  return actions.length === 0 || actions.includes(action);
}
