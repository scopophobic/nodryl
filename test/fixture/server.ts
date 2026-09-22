import express from 'express';
import http from 'node:http';

const app = express();
const port = Number(process.env.PORT || 3000);

app.get('/checkout', async (_request, response) => {
  try {
    const result = await new Promise<number>((resolve, reject) => {
      http.get(`http://127.0.0.1:${port}/payment`, payment => {
        payment.resume();
        payment.once('end', () => resolve(payment.statusCode || 500));
      }).once('error', reject);
    });
    response.status(result).json({ ok: result === 200 });
  } catch {
    response.status(500).json({ ok: false });
  }
});

app.get('/payment', (_request, response) => response.json({ paid: true }));

app.listen(port, '127.0.0.1', () => console.log(`READY ${port}`));
