import asyncio
from typing import AsyncGenerator, List, Callable


async def batch_process(items: List[Any], processor: Callable, batch_size: int = 10):
    """Process items in async batches."""

    for i in range(0, len(items), batch_size):
        batch = items[i:i + batch_size]
        tasks = [processor(item) for item in batch]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        yield results


async def retry_async(func: Callable, max_attempts: int = 3, delay: float = 1.0):
    """Retry an async function with exponential backoff."""

    last_exc = None
    for attempt in range(max_attempts):
        try:
            return await func()
        except Exception as e:
            last_exc = e
            if attempt < max_attempts - 1:
                await asyncio.sleep(delay * (2 ** attempt))
    raise last_exc


async def stream_events(source: AsyncGenerator):
    """Consume an async generator and collect events."""

    events = []
    async for event in source:
        events.append(event)
    return events
